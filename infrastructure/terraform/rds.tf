resource "aws_db_subnet_group" "public" {
  name        = "${var.project_name}-db-public-subnet-group"
  description = "Public database subnet group for ${var.project_name}"
  subnet_ids  = aws_subnet.public[*].id

  tags = {
    Name = "${var.project_name}-db-public-subnet-group"
  }
}

resource "aws_db_instance" "main" {
  identifier = "${var.project_name}-postgres"

  engine         = "postgres"
  engine_version = "16.3"
  instance_class = var.db_instance_class

  allocated_storage     = 20
  max_allocated_storage = 20
  storage_type          = "gp2"
  storage_encrypted     = true

  db_name                       = var.db_name
  username                      = var.db_username
  manage_master_user_password   = true
  master_user_secret_kms_key_id = aws_kms_key.application_secrets.arn
  port                          = var.db_port

  db_subnet_group_name   = aws_db_subnet_group.public.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  publicly_accessible    = var.db_publicly_accessible
  multi_az               = false

  backup_retention_period = var.environment == "prod" ? 3 : 1
  backup_window           = "03:00-04:00"
  maintenance_window      = "Mon:04:00-Mon:05:00"

  performance_insights_enabled = false

  deletion_protection       = var.environment == "prod" ? true : false
  skip_final_snapshot       = var.environment != "prod"
  final_snapshot_identifier = var.environment == "prod" ? "${var.project_name}-final-snapshot" : null

  tags = {
    Name = "${var.project_name}-postgres"
  }
}
