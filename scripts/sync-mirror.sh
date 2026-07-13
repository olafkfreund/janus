#!/usr/bin/env bash
#
# sync-mirror.sh — push a branch to BOTH Janus remotes after a commit+merge.
#
#   origin     -> github.com/olafkfreund/janus        (primary; pushing main
#                                                       triggers the live EKS deploy)
#   synechron  -> github.com/synechron/Janus-mcp-gateway  (Synechron mirror)
#
# Run this AFTER the PR is merged on origin/main (so the origin push is a no-op
# fast-forward) to keep the Synechron mirror in sync.
#
# The synechron org enforces SAML SSO (enterprise "synechrontech"), and
# `gh auth setup-git` installs a github.com credential helper that injects the
# wrong (non-SSO) token — git then masks the failure as "Repository not found".
# This script works around both: it pushes with the credential helper disabled
# and supplies an SSO-authorized token via GIT_ASKPASS. The token never appears
# on a command line, in git config, or in the remote URL.
#
# Usage:
#   scripts/sync-mirror.sh [ref]          # ref defaults to "main"
#
# Env:
#   SYNECHRON_TOKEN_FILE   path to a file holding an SSO-authorized ghp_ PAT
#                          (default: /tmp/synechron_github.txt)
#   SYNECHRON_REMOTE_URL   override the mirror URL
#   SKIP_ORIGIN=1          push only the Synechron mirror

set -euo pipefail

REF="${1:-main}"
TOKEN_FILE="${SYNECHRON_TOKEN_FILE:-/tmp/synechron_github.txt}"
SYNECHRON_URL="${SYNECHRON_REMOTE_URL:-https://github.com/synechron/Janus-mcp-gateway.git}"

# Redact any token that might slip into git's output.
redact() { sed -E 's/ghp_[A-Za-z0-9]+/ghp_REDACTED/g; s/authorization_request=[A-Z0-9]+/authorization_request=REDACTED/g'; }

# 1. origin (normal credentials; no-op if already merged/up to date).
if [ "${SKIP_ORIGIN:-0}" != "1" ]; then
  echo ">> pushing ${REF} -> origin"
  git push origin "${REF}:${REF}" 2>&1 | redact
fi

# 2. synechron mirror (SSO token via askpass, gh credential helper bypassed).
# Prefer the SYNECHRON_TOKEN env var (e.g. exported from .envrc), else read the
# token file.
if [ -n "${SYNECHRON_TOKEN:-}" ]; then
  SYN_TOKEN="$(printf '%s' "${SYNECHRON_TOKEN}" | tr -d '\r\n')"
elif [ -s "${TOKEN_FILE}" ]; then
  SYN_TOKEN="$(tr -d '\r\n' < "${TOKEN_FILE}")"
else
  echo "ERROR: no Synechron token available." >&2
  echo "       Set SYNECHRON_TOKEN (e.g. in .envrc) or point SYNECHRON_TOKEN_FILE" >&2
  echo "       at a file holding an SSO-authorized ghp_ PAT for the synechron org." >&2
  exit 1
fi
export SYN_TOKEN

ASKPASS="$(mktemp)"
trap 'rm -f "${ASKPASS}"' EXIT
printf '%s\n' '#!/bin/sh' 'printf "%s" "$SYN_TOKEN"' > "${ASKPASS}"
chmod 700 "${ASKPASS}"

echo ">> pushing ${REF} -> synechron mirror"
set +e
GIT_ASKPASS="${ASKPASS}" GIT_TERMINAL_PROMPT=0 \
  git -c credential.helper= -c credential.https://github.com.helper= \
  push "https://x-access-token@${SYNECHRON_URL#https://}" "${REF}:${REF}" 2>&1 | redact
status="${PIPESTATUS[0]}"
set -e

if [ "${status}" -ne 0 ]; then
  echo "ERROR: push to Synechron mirror failed (exit ${status})." >&2
  echo "       If it says 'Repository not found', the token is not SSO-authorized for the" >&2
  echo "       synechron org. Authorize it (token's 'Configure SSO' page / enterprise SSO link)" >&2
  echo "       and re-run. See memory: dual-remote-push." >&2
  exit "${status}"
fi

echo "OK: ${REF} synced to origin + synechron."
