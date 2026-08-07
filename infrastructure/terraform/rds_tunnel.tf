locals {
  rds_tunnel_enabled = var.enable_rds_tunnel && !contains(["prod", "production"], lower(var.environment))
}

data "aws_ami" "amazon_linux_2023" {
  count       = local.rds_tunnel_enabled ? 1 : 0
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_iam_policy_document" "rds_tunnel_assume_role" {
  count = local.rds_tunnel_enabled ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "rds_tunnel" {
  count                = local.rds_tunnel_enabled ? 1 : 0
  name                 = "${local.resource_prefix}-rds-tunnel-role"
  assume_role_policy   = data.aws_iam_policy_document.rds_tunnel_assume_role[0].json
  permissions_boundary = local.workload_permissions_boundary_arn
}

resource "aws_iam_role_policy_attachment" "rds_tunnel_ssm" {
  count      = local.rds_tunnel_enabled ? 1 : 0
  role       = aws_iam_role.rds_tunnel[0].name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "rds_tunnel" {
  count = local.rds_tunnel_enabled ? 1 : 0
  name  = "${local.resource_prefix}-rds-tunnel-profile"
  role  = aws_iam_role.rds_tunnel[0].name
}

resource "aws_security_group" "rds_tunnel" {
  count       = local.rds_tunnel_enabled ? 1 : 0
  name        = "${local.resource_prefix}-rds-tunnel-sg"
  description = "${local.resource_prefix} outbound-only security group for the SSM RDS tunnel host"
  vpc_id      = aws_vpc.main.id

  egress {
    description = "Allow outbound traffic for SSM and private RDS access"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.resource_prefix}-rds-tunnel-sg"
  }
}

resource "aws_instance" "rds_tunnel" {
  count                       = local.rds_tunnel_enabled ? 1 : 0
  ami                         = data.aws_ami.amazon_linux_2023[0].id
  instance_type               = var.rds_tunnel_instance_type
  subnet_id                   = aws_subnet.private[0].id
  associate_public_ip_address = false
  iam_instance_profile        = aws_iam_instance_profile.rds_tunnel[0].name
  vpc_security_group_ids      = [aws_security_group.rds_tunnel[0].id]

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  tags = {
    Name = "${local.resource_prefix}-rds-tunnel"
  }

  depends_on = [
    aws_iam_role_policy_attachment.rds_tunnel_ssm,
    aws_route.private_default_egress
  ]
}
