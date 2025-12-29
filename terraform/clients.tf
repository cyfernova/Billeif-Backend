resource "aws_dynamodb_table" "clients" {
  name         = "invoice-storage-clients"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }

  attribute {
    name = "email"
    type = "S"
  }

  global_secondary_index {
    name            = "email-index"
    hash_key        = "email"
    projection_type = "ALL"
  }

  tags = {
    Name        = "invoice-storage-clients"
    Environment = "development"
  }
}

output "clients_table_name" {
  value       = aws_dynamodb_table.clients.name
  description = "DynamoDB clients table name"
}

output "clients_table_arn" {
  value       = aws_dynamodb_table.clients.arn
  description = "DynamoDB clients table ARN"
}
