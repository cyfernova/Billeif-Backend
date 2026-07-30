# VPC
resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "${local.resource_prefix}-vpc"
  }
}

# Internet Gateway
resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.resource_prefix}-igw"
  }
}

# Public Subnets
resource "aws_subnet" "public" {
  count                   = length(var.availability_zones)
  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 4, count.index)
  availability_zone       = var.availability_zones[count.index]
  map_public_ip_on_launch = true

  tags = {
    Name = "${local.resource_prefix}-public-${count.index + 1}"
    Type = "public"
  }
}

# Private Subnets for Lambda workloads
resource "aws_subnet" "private" {
  count             = length(var.availability_zones)
  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 4, count.index + length(var.availability_zones))
  availability_zone = var.availability_zones[count.index]

  tags = {
    Name = "${local.resource_prefix}-private-${count.index + 1}"
    Type = "private"
  }
}

# Isolated subnets host only the Billeif database. They have no default route
# to the Internet Gateway or NAT instance.
resource "aws_subnet" "database" {
  count                   = length(var.availability_zones)
  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 4, count.index + (2 * length(var.availability_zones)))
  availability_zone       = var.availability_zones[count.index]
  map_public_ip_on_launch = false

  tags = {
    Name = "${local.resource_prefix}-database-isolated-${count.index + 1}"
    Type = "database-isolated"
  }
}

# Public Route Table
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${local.resource_prefix}-public-rt"
  }
}

# Route Table Associations - Public
resource "aws_route_table_association" "public" {
  count          = length(var.availability_zones)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  domain = "vpc"

  tags = {
    Name = "${local.resource_prefix}-nat-eip"
  }

  depends_on = [aws_internet_gateway.main]
}

resource "aws_nat_gateway" "main" {
  count         = var.egress_mode == "managed_nat" ? 1 : 0
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[0].id

  tags = {
    Name = "${local.resource_prefix}-managed-nat"
  }

  depends_on = [aws_internet_gateway.main]
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.resource_prefix}-private-rt"
  }
}

resource "aws_route_table_association" "private" {
  count          = length(var.availability_zones)
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table" "database" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.resource_prefix}-database-isolated-rt"
  }
}

resource "aws_route_table_association" "database" {
  count          = length(var.availability_zones)
  subnet_id      = aws_subnet.database[count.index].id
  route_table_id = aws_route_table.database.id
}

resource "aws_security_group" "lambda" {
  name        = "${local.resource_prefix}-lambda-sg"
  description = "${local.resource_prefix} security group for Lambda functions"
  vpc_id      = aws_vpc.main.id

  dynamic "ingress" {
    for_each = var.enable_rds_proxy ? [1] : []

    content {
      description = "Billeif RDS Proxy from Lambda"
      from_port   = var.db_port
      to_port     = var.db_port
      protocol    = "tcp"
      self        = true
    }
  }

  dynamic "ingress" {
    for_each = var.enable_rds_proxy ? [aws_security_group.database_migrator.id] : []

    content {
      description     = "Billeif RDS Proxy from database migrator"
      from_port       = var.db_port
      to_port         = var.db_port
      protocol        = "tcp"
      security_groups = [ingress.value]
    }
  }

  dynamic "ingress" {
    for_each = var.enable_rds_proxy && local.rds_tunnel_enabled ? [aws_security_group.rds_tunnel[0].id] : []

    content {
      description     = "Billeif RDS Proxy from RDS tunnel"
      from_port       = var.db_port
      to_port         = var.db_port
      protocol        = "tcp"
      security_groups = [ingress.value]
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.resource_prefix}-lambda-sg"
  }
}

resource "aws_security_group" "database_migrator" {
  name        = "${local.resource_prefix}-database-migrator-sg"
  description = "${local.resource_prefix} security group for the database migration Lambda"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.resource_prefix}-database-migrator-sg"
  }
}

# Security Group for RDS
resource "aws_security_group" "rds" {
  name        = "${local.resource_prefix}-rds-sg"
  description = "${local.resource_prefix} security group for RDS PostgreSQL"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "PostgreSQL from Lambda"
    from_port       = var.db_port
    to_port         = var.db_port
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  ingress {
    description     = "PostgreSQL from database migrator"
    from_port       = var.db_port
    to_port         = var.db_port
    protocol        = "tcp"
    security_groups = [aws_security_group.database_migrator.id]
  }

  dynamic "ingress" {
    for_each = local.rds_tunnel_enabled ? [aws_security_group.rds_tunnel[0].id] : []

    content {
      description     = "PostgreSQL from Billeif RDS tunnel"
      from_port       = var.db_port
      to_port         = var.db_port
      protocol        = "tcp"
      security_groups = [ingress.value]
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${local.resource_prefix}-rds-sg"
  }
}
