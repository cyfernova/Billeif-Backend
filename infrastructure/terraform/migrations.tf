# Database migrations
# NOTE: This is a simplified migration approach. For production, use a proper
# migration tool like golang-migrate or Flyway as part of your CI/CD pipeline.

resource "null_resource" "add_categories_column" {
  triggers = {
    migration_version = timestamp()
  }

  provisioner "local-exec" {
    command = <<-EOT
      echo "Running migration: Adding categories column to products table..."
      PGPASSWORD="${var.db_password}" psql \
        -h "${aws_db_instance.main.address}" \
        -U "${var.db_username}" \
        -d "${var.db_name}" \
        -p "${var.db_port}" \
        -c "ALTER TABLE products ADD COLUMN IF NOT EXISTS categories TEXT[] DEFAULT '{}';"
      PGPASSWORD="${var.db_password}" psql \
        -h "${aws_db_instance.main.address}" \
        -U "${var.db_username}" \
        -d "${var.db_name}" \
        -p "${var.db_port}" \
        -c "CREATE INDEX IF NOT EXISTS idx_products_categories ON products USING GIN (categories);"
      echo "Migration completed successfully."
    EOT
  }
}

resource "null_resource" "add_categories_to_agents" {
  depends_on = [null_resource.add_categories_column]

  triggers = {
    migration_version = timestamp()
  }

  provisioner "local-exec" {
    command = <<-EOT
      echo "Running migration: Adding categories column to agents table..."
      PGPASSWORD="${var.db_password}" psql \
        -h "${aws_db_instance.main.address}" \
        -U "${var.db_username}" \
        -d "${var.db_name}" \
        -p "${var.db_port}" \
        -c "ALTER TABLE agents ADD COLUMN IF NOT EXISTS categories TEXT[] DEFAULT '{}';"
      PGPASSWORD="${var.db_password}" psql \
        -h "${aws_db_instance.main.address}" \
        -U "${var.db_username}" \
        -d "${var.db_name}" \
        -p "${var.db_port}" \
        -c "CREATE INDEX IF NOT EXISTS idx_agents_categories ON agents USING GIN (categories);"
      echo "Migration completed successfully."
    EOT
  }
}
