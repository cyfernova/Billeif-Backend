resource "aws_dynamodb_table" "invoices" {
  name         = "invoice-storage-invoices"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }

  attribute {
    name = "client_id"
    type = "S"
  }

  attribute {
    name = "status"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "S"
  }

  global_secondary_index {
    name            = "client_id-index"
    hash_key        = "client_id"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "status-index"
    hash_key        = "status"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "created_at-index"
    hash_key        = "id"
    range_key       = "created_at"
    projection_type = "ALL"
  }

  tags = {
    Name        = "invoice-storage-invoices"
    Environment = "development"
  }
}

output "invoices_table_name" {
  value       = aws_dynamodb_table.invoices.name
  description = "DynamoDB invoices table name"
}

output "invoices_table_arn" {
  value       = aws_dynamodb_table.invoices.arn
  description = "DynamoDB invoices table ARN"
}
