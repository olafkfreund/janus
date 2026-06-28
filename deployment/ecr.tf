resource "aws_ecr_repository" "gateway" {
  name                 = "mcp-api-gateway"
  image_tag_mutability = "MUTABLE"

  # Secure settings: Enforce vulnerability scan on image push
  image_scanning_configuration {
    scan_on_push = true
  }

  # Enforce KMS encryption (AWS managed key is standard and cost-effective)
  encryption_configuration {
    encryption_type = "KMS"
  }
}

# Best practice: Lifecycle policy to prune old images and prevent infinite storage costs
resource "aws_ecr_lifecycle_policy" "gateway" {
  repository = aws_ecr_repository.gateway.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Keep only the last 30 built images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 30
        }
        action = {
          type = "expire"
        }
      }
    ]
  })
}
