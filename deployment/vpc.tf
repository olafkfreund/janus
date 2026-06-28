data "aws_availability_zones" "available" {
  state = "available"
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"

  name = "${var.environment}-mcp-vpc"
  cidr = var.vpc_cidr

  # Use availability zones dynamically from selected region
  azs             = slice(data.aws_availability_zones.available.names, 0, 2)
  private_subnets = var.private_subnets
  public_subnets  = var.public_subnets

  # NAT Gateway configuration for EKS private worker nodes egress
  enable_nat_gateway     = true
  single_nat_gateway     = true # Cost-efficiency for small cluster / development environment
  one_nat_gateway_per_az = false

  enable_dns_hostnames = true
  enable_dns_support   = true

  # Tag subnets for EKS ingress and integration
  public_subnet_tags = {
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
    "kubernetes.io/role/elb"                    = "1"
  }

  private_subnet_tags = {
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
    "kubernetes.io/role/internal-elb"           = "1"
  }
}
