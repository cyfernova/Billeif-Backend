resource "aws_dynamodb_table" "users_sessions" {
  name         = "${local.resource_prefix}-users-sessions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  range_key    = "session_id"

  attribute {
    name = "user_id"
    type = "S"
  }

  attribute {
    name = "session_id"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = false
  }
}

resource "aws_dynamodb_table" "refresh_tokens" {
  name         = "${local.resource_prefix}-refresh-tokens"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "token"

  attribute {
    name = "token"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "password_reset_tokens" {
  name         = "${local.resource_prefix}-password-reset-tokens"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "token"

  attribute {
    name = "token"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "mfa_codes" {
  name         = "${local.resource_prefix}-mfa-codes"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  range_key    = "code"

  attribute {
    name = "user_id"
    type = "S"
  }

  attribute {
    name = "code"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "phone_auth_cooldowns" {
  name         = local.phone_auth_cooldown_table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "cooldown_key"

  attribute {
    name = "cooldown_key"
    type = "S"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "customers_cache" {
  name         = "${local.resource_prefix}-customers-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "customer_id"

  attribute {
    name = "customer_id"
    type = "S"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "business_id"
    type = "S"
  }
}

resource "aws_dynamodb_table" "vendors_cache" {
  name         = "${local.resource_prefix}-vendors-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "vendor_id"

  attribute {
    name = "vendor_id"
    type = "S"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "business_id"
    type = "S"
  }
}

resource "aws_dynamodb_table" "products_cache" {
  name         = "${local.resource_prefix}-products-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "product_id"

  attribute {
    name = "product_id"
    type = "S"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  global_secondary_index {
    name = "sku_index"
    key_schema {
      attribute_name = "sku"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "business_id"
    type = "S"
  }

  attribute {
    name = "sku"
    type = "S"
  }
}

resource "aws_dynamodb_table" "invoices_cache" {
  name         = "${local.resource_prefix}-invoices-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "invoice_id"

  attribute {
    name = "invoice_id"
    type = "S"
  }

  global_secondary_index {
    name = "invoice_no_index"
    key_schema {
      attribute_name = "invoice_no"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "invoice_no"
    type = "S"
  }

  attribute {
    name = "business_id"
    type = "S"
  }
}

resource "aws_dynamodb_table" "ledger_cache" {
  name         = "${local.resource_prefix}-ledger-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "ledger_entry_id"

  attribute {
    name = "ledger_entry_id"
    type = "S"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "business_id"
    type = "S"
  }
}

resource "aws_dynamodb_table" "payments_cache" {
  name         = "${local.resource_prefix}-payments-cache"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "payment_id"

  attribute {
    name = "payment_id"
    type = "S"
  }

  global_secondary_index {
    name = "business_id_index"
    key_schema {
      attribute_name = "business_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  attribute {
    name = "business_id"
    type = "S"
  }
}

# WebSocket connection registry
resource "aws_dynamodb_table" "ws_connections" {
  name         = local.websocket_connections_table
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "connection_id"

  attribute {
    name = "connection_id"
    type = "S"
  }

  attribute {
    name = "user_id"
    type = "S"
  }

  global_secondary_index {
    name = "user_id-index"
    key_schema {
      attribute_name = "user_id"
      key_type       = "HASH"
    }
    projection_type = "ALL"
  }

  ttl {
    attribute_name = "ttl"
    enabled        = false
  }

  point_in_time_recovery {
    enabled = false
  }

  tags = {
    Name = local.websocket_connections_table
  }
}

resource "aws_dynamodb_table" "voice_sessions" {
  name         = local.voice_sessions_table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  attribute {
    name = "GSI1PK"
    type = "S"
  }

  attribute {
    name = "GSI1SK"
    type = "S"
  }

  attribute {
    name = "GSI2PK"
    type = "S"
  }

  attribute {
    name = "GSI2SK"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi1"
    hash_key        = "GSI1PK"
    range_key       = "GSI1SK"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "gsi2"
    hash_key        = "GSI2PK"
    range_key       = "GSI2SK"
    projection_type = "ALL"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = var.environment == "prod"
  }

  tags = {
    Name = local.voice_sessions_table_name
  }
}
