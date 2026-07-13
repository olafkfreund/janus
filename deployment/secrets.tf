# AWS Secrets Manager Secret to house gateway environment variables securely
resource "aws_secretsmanager_secret" "gateway" {
  name        = "${var.environment}-mcp-gateway-secrets"
  description = "Sensitive configuration secrets for the MCP API Gateway"

  # Allow simple deletion for PoC, best practice is to have recovery window in production
  recovery_window_in_days = var.environment == "dev" ? 0 : 30
}

# Generate strong random secrets at apply time. Never commit real secret
# material to version control.
resource "random_password" "jwt_secret" {
  length  = 48
  special = false
}

resource "random_password" "gateway_token" {
  length  = 48
  special = false
}

# Seed the secret with generated values. `ignore_changes` ensures secrets rotated
# out-of-band (console / rotation lambda) are not clobbered on subsequent applies.
resource "aws_secretsmanager_secret_version" "gateway" {
  secret_id = aws_secretsmanager_secret.gateway.id
  secret_string = jsonencode({
    jwt-secret    = random_password.jwt_secret.result
    gateway-token = random_password.gateway_token.result
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# Account/region used to scope the vault namespace ARN below.
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# Namespace prefix under which the "aws" vault provider stores downstream API
# credentials. The gateway is deployed with VAULT_PROVIDER=aws and
# AWS_SECRETS_PREFIX="${var.environment}-mcp-vault/" so its secrets live under
# this prefix, isolated from the gateway's own bootstrap secret above.
locals {
  vault_secret_arn_prefix = "arn:aws:secretsmanager:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:secret:${var.environment}-mcp-vault/*"
}

# IAM Policy for secrets resolution
resource "aws_iam_policy" "secrets_read" {
  name        = "${var.environment}-mcp-secrets-policy"
  description = "Allows EKS pods to read gateway bootstrap secrets and manage the 'aws' vault namespace in Secrets Manager"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "GatewayBootstrapSecretRead"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [
          aws_secretsmanager_secret.gateway.arn
        ]
      },
      {
        # The "aws" vault provider manages downstream API credentials as
        # individual secrets under the vault namespace prefix. Read is the hot
        # path (tool calls); create/put/delete/list back the portal + CLI vault
        # management. Scoped to the prefix so the gateway can never touch other
        # secrets in the account.
        Sid    = "AwsVaultNamespaceManage"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret",
          "secretsmanager:CreateSecret",
          "secretsmanager:PutSecretValue",
          "secretsmanager:DeleteSecret"
        ]
        Resource = [
          local.vault_secret_arn_prefix
        ]
      },
      {
        # ListSecrets does not support resource-level scoping in IAM; a filter
        # on the caller side (AWS_SECRETS_PREFIX) restricts what the portal
        # actually displays.
        Sid      = "AwsVaultList"
        Effect   = "Allow"
        Action   = ["secretsmanager:ListSecrets"]
        Resource = ["*"]
      }
    ]
  })
}

# EKS Service Account IAM Role (IRSA) for Pod Identity Federation
resource "aws_iam_role" "gateway_sa" {
  name = "${var.environment}-mcp-gateway-sa-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Federated = module.eks.oidc_provider_arn
        }
        Action = "sts:AssumeRoleWithWebIdentity"
        Condition = {
          StringEquals = {
            "${module.eks.oidc_provider}:sub" = "system:serviceaccount:default:mcp-api-gateway-sa"
            "${module.eks.oidc_provider}:aud" = "sts.amazonaws.com"
          }
        }
      }
    ]
  })
}

# Attach secrets access policy to the IRSA role
resource "aws_iam_role_policy_attachment" "gateway_sa_secrets" {
  role       = aws_iam_role.gateway_sa.name
  policy_arn = aws_iam_policy.secrets_read.arn
}
