output "vpc_id" {
  description = "The ID of the provisioned VPC"
  value       = module.vpc.vpc_id
}

output "eks_cluster_name" {
  description = "The name of the provisioned EKS cluster"
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "The endpoint URL for the EKS cluster API"
  value       = module.eks.cluster_endpoint
}

output "eks_cluster_arn" {
  description = "The ARN of the EKS cluster"
  value       = module.eks.cluster_arn
}

output "ecr_repository_url" {
  description = "The registry URL of the ECR repository"
  value       = aws_ecr_repository.gateway.repository_url
}

output "secretsmanager_secret_arn" {
  description = "The ARN of the AWS Secrets Manager Secret"
  value       = aws_secretsmanager_secret.gateway.arn
}

output "secretsmanager_secret_name" {
  description = "The Name of the AWS Secrets Manager Secret"
  value       = aws_secretsmanager_secret.gateway.name
}

output "irsa_role_arn" {
  description = "The IAM Role ARN configured for EKS Service Account Pod Identity (IRSA)"
  value       = aws_iam_role.gateway_sa.arn
}

output "kubeconfig_command" {
  description = "Command to configure kubectl credentials locally"
  value       = "aws eks update-kubeconfig --name ${module.eks.cluster_name} --region ${var.aws_region}"
}
