locals {
  nat_instance_enabled = var.egress_mode == "nat_instance"
  private_subnet_cidrs = [
    for index in range(length(var.availability_zones)) :
    cidrsubnet(var.vpc_cidr, 4, index + length(var.availability_zones))
  ]
}

data "aws_ami" "billeif_nat_instance" {
  count       = local.nat_instance_enabled ? 1 : 0
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-arm64"]
  }

  filter {
    name   = "architecture"
    values = ["arm64"]
  }

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_iam_policy_document" "nat_instance_assume_role" {
  count = local.nat_instance_enabled ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "nat_instance" {
  count              = local.nat_instance_enabled ? 1 : 0
  name               = "${local.resource_prefix}-nat-instance-role"
  description        = "Billeif NAT instance role for SSM access"
  assume_role_policy = data.aws_iam_policy_document.nat_instance_assume_role[0].json
}

resource "aws_iam_role_policy_attachment" "nat_instance_ssm" {
  count      = local.nat_instance_enabled ? 1 : 0
  role       = aws_iam_role.nat_instance[0].name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "nat_instance" {
  count = local.nat_instance_enabled ? 1 : 0
  name  = "${local.resource_prefix}-nat-instance-profile"
  role  = aws_iam_role.nat_instance[0].name
}

resource "aws_security_group" "nat_instance" {
  count       = local.nat_instance_enabled ? 1 : 0
  name        = "${local.resource_prefix}-nat-instance-sg"
  description = "Billeif NAT forwarding from private application subnets"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "Billeif forwarding from private application subnets"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = local.private_subnet_cidrs
  }

  egress {
    description = "Billeif NAT outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.resource_prefix}-nat-instance-sg"
  }
}

resource "aws_instance" "nat" {
  count                       = local.nat_instance_enabled ? 1 : 0
  ami                         = data.aws_ami.billeif_nat_instance[0].id
  instance_type               = "t4g.micro"
  subnet_id                   = aws_subnet.public[0].id
  associate_public_ip_address = false
  source_dest_check           = false
  monitoring                  = false
  iam_instance_profile        = aws_iam_instance_profile.nat_instance[0].name
  vpc_security_group_ids      = [aws_security_group.nat_instance[0].id]
  user_data = templatefile("${path.module}/templates/nat-instance-user-data.sh.tftpl", {
    vpc_cidr = var.vpc_cidr
  })
  user_data_replace_on_change = true

  metadata_options {
    http_endpoint               = "enabled"
    http_protocol_ipv6          = "disabled"
    http_put_response_hop_limit = 1
    http_tokens                 = "required"
    instance_metadata_tags      = "disabled"
  }

  credit_specification {
    cpu_credits = "standard"
  }

  root_block_device {
    encrypted             = true
    volume_type           = "gp3"
    volume_size           = 8
    delete_on_termination = true
  }

  tags = {
    Name = "${local.resource_prefix}-nat-instance"
  }

  depends_on = [
    aws_iam_role_policy_attachment.nat_instance_ssm,
    aws_internet_gateway.main
  ]
}

resource "aws_eip" "nat_instance" {
  count  = local.nat_instance_enabled ? 1 : 0
  domain = "vpc"

  tags = {
    Name = "${local.resource_prefix}-nat-instance-eip"
  }

  depends_on = [aws_internet_gateway.main]
}

resource "aws_eip_association" "nat_instance" {
  count         = local.nat_instance_enabled ? 1 : 0
  allocation_id = aws_eip.nat_instance[0].id
  instance_id   = aws_instance.nat[0].id
}

resource "aws_route" "private_default_egress" {
  route_table_id         = aws_route_table.private.id
  destination_cidr_block = "0.0.0.0/0"
  network_interface_id   = local.nat_instance_enabled ? aws_instance.nat[0].primary_network_interface_id : null
  nat_gateway_id         = var.egress_mode == "managed_nat" ? aws_nat_gateway.main[0].id : null

  depends_on = [
    aws_eip_association.nat_instance,
    aws_nat_gateway.main
  ]
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.aws_region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = {
    Name = "${local.resource_prefix}-s3-gateway-endpoint"
  }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${var.aws_region}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = {
    Name = "${local.resource_prefix}-dynamodb-gateway-endpoint"
  }
}

resource "aws_cloudwatch_metric_alarm" "nat_system_status" {
  count               = local.nat_instance_enabled ? 1 : 0
  alarm_name          = "${local.resource_prefix}-nat-system-status"
  alarm_description   = "Billeif NAT instance system status failed"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  statistic           = "Maximum"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 1
  period              = 60
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions = [
    "arn:${data.aws_partition.current.partition}:automate:${var.aws_region}:ec2:recover",
    aws_sns_topic.alerts.arn
  ]
  ok_actions = [aws_sns_topic.alerts.arn]

  dimensions = {
    InstanceId = aws_instance.nat[0].id
  }
}

resource "aws_cloudwatch_metric_alarm" "nat_cpu_high" {
  count               = local.nat_instance_enabled ? 1 : 0
  alarm_name          = "${local.resource_prefix}-nat-cpu-high"
  alarm_description   = "Billeif NAT instance CPU utilization is high"
  namespace           = "AWS/EC2"
  metric_name         = "CPUUtilization"
  statistic           = "Average"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 70
  period              = 300
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    InstanceId = aws_instance.nat[0].id
  }
}

resource "aws_cloudwatch_metric_alarm" "nat_cpu_credits_low" {
  count               = local.nat_instance_enabled ? 1 : 0
  alarm_name          = "${local.resource_prefix}-nat-cpu-credits-low"
  alarm_description   = "Billeif NAT instance CPU credit balance is low"
  namespace           = "AWS/EC2"
  metric_name         = "CPUCreditBalance"
  statistic           = "Minimum"
  comparison_operator = "LessThanOrEqualToThreshold"
  threshold           = 20
  period              = 300
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  dimensions = {
    InstanceId = aws_instance.nat[0].id
  }
}
