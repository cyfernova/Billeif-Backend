resource "aws_db_subnet_group" "public" {
  name        = "${local.resource_prefix}-db-isolated-subnet-group"
  description = "Isolated Billeif database subnet group for ${local.resource_prefix}"
  subnet_ids  = aws_subnet.database[*].id

  tags = {
    Name = "${local.resource_prefix}-db-isolated-subnet-group"
  }
}

resource "aws_db_instance" "main" {
  identifier = "${local.resource_prefix}-postgres"

  engine         = "postgres"
  engine_version = "18.4"
  instance_class = var.db_instance_class

  allocated_storage     = 20
  max_allocated_storage = 100
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name                       = var.db_name
  username                      = var.db_username
  manage_master_user_password   = true
  master_user_secret_kms_key_id = aws_kms_key.application_secrets.arn
  port                          = var.db_port

  db_subnet_group_name   = aws_db_subnet_group.public.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  publicly_accessible    = false
  multi_az               = var.db_multi_az

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "Mon:04:00-Mon:05:00"

  database_insights_mode       = "standard"
  performance_insights_enabled = false

  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${local.resource_prefix}-final-snapshot"
  copy_tags_to_snapshot     = true

  tags = {
    Name = "${local.resource_prefix}-postgres"
  }
}
