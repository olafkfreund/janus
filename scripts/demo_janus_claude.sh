#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -e

# Terminal colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

clear
echo -e "${BLUE}================================================================${NC}"
echo -e "${BLUE}       Janus Gateway: AWS EKS & Claude Code integration Demo    ${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e ""
echo -e "${CYAN}[1/4] Architecture Overview:${NC}"
echo -e "  * Gateway Facade: Deployed under namespace ${GREEN}janus${NC} on AWS EKS cluster."
echo -e "  * SSL/TLS Endpoint: ${GREEN}https://janus.13.134.88.9.nip.io/sse${NC} (Let's Encrypt)"
echo -e "  * Authentication: Secure Bearer Token validation."
echo -e "  * Downstream Services: Dynamic routing to LCH clearing and Treasury yield APIs."
echo -e ""
sleep 2

echo -e "${CYAN}[2/4] Claude Code MCP Configuration (.mcp.json):${NC}"
echo -e "Showing the project MCP config that registers Janus as a remote SSE server..."
echo -e ""
echo -e "${YELLOW}"
cat .mcp.json
echo -e "${NC}"
sleep 2

# Allow overriding the gateway token without editing .mcp.json
: "${JANUS_GATEWAY_TOKEN:=highly-secure-mcp-bearer-token-key-for-llm-clients}"
export JANUS_GATEWAY_TOKEN

if ! command -v claude >/dev/null 2>&1; then
  echo -e "${YELLOW}! 'claude' CLI not found. Install Claude Code, then re-run.${NC}"
  echo -e "  https://docs.claude.com/en/docs/claude-code"
  exit 1
fi

echo -e "${CYAN}[3/4] Triggering Claude Code (headless) agent session...${NC}"
echo -e "The agent will (all via the single governed gateway):"
echo -e "  1. Load the ${GREEN}janus-gateway${NC} remote MCP tools via EKS (.mcp.json)."
echo -e "  2. Pull LCH collateral + multi-jurisdiction rates: ${GREEN}US Treasury, Bank of England, ECB FX, Eurostat${NC}."
echo -e "  3. Translate the multi-currency portfolio to GBP and compile a cross-currency margin audit."
echo -e ""
PROMPT="Using ONLY the janus-gateway MCP tools (do not fabricate any figures), produce a Cross-Currency Collateral Valuation & Multi-Jurisdiction Rate Audit for LCH clearing member MEM-LCH-002. Steps: (1) fetch the member's non-cash collateral via lch_get_non_cash_collateral; (2) fetch US Treasury average interest rates via ustreasury_get_avg_interest_rates; (3) fetch the UK Bank of England Bank Rate via boe_get_bank_rate (CSV, take the latest value); (4) fetch euro-area HICP inflation via eurostat_get_hicp_inflation with geo EA; (5) fetch ECB euro FX reference rates via fx_get_reference_rates with base EUR and symbols USD,GBP. Then reconcile each asset's post-haircut value, translate the multi-currency portfolio into a single GBP reporting value using the ECB FX rates, benchmark the collateral against the US/UK/EU reference rates, add euro-area inflation context, and produce a structured markdown margin audit report with a clear consolidated GBP figure."
echo -e "${MAGENTA}Running: claude -p \"<cross-currency report prompt>\" --mcp-config .mcp.json --allowedTools \"mcp__janus-gateway\"${NC}"
echo -e ""

# Use the claude.ai subscription, not ANTHROPIC_API_KEY: Claude Code prefers the
# API key when it is set in the env, which can hit API usage limits. Unset it for
# this invocation only (other tools that rely on the key are unaffected).
env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN claude -p "$PROMPT" \
  --mcp-config .mcp.json \
  --allowedTools "mcp__janus-gateway"

echo -e ""
echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}✓ Demo completed successfully!${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e "How to run interactively in the TUI:"
echo -e "  1. Start Claude Code in this directory: ${CYAN}claude${NC}"
echo -e "  2. Approve the ${GREEN}janus-gateway${NC} MCP server when prompted (project .mcp.json)."
echo -e "  3. Type ${CYAN}/mcp${NC} to view registered Janus gateway tools."
echo -e "  4. Ask: ${CYAN}\"Generate collateral report for MEM-LCH-002\"${NC}"
echo -e "${BLUE}================================================================${NC}"
