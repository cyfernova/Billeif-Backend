resource "aws_dynamodb_table" "users_sessions" {
  name         = "users_sessions"
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
  name         = "refresh_tokens"
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
  name         = "password_reset_tokens"
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
  name         = "mfa_codes"
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
  name         = var.phone_auth_cooldown_table_name
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
  name         = "customers_cache"
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
  name         = "vendors_cache"
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
  name         = "products_cache"
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
  name         = "invoices_cache"
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

resource "aws_dynamodb_table" "invoice_sequences" {
  name         = "invoice_sequences"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "business_id"
  range_key    = "invoice_type"

  attribute {
    name = "business_id"
    type = "S"
  }

  attribute {
    name = "invoice_type"
    type = "S"
  }
}

resource "aws_dynamodb_table" "ledger_cache" {
  name         = "ledger_cache"
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
  name         = "payments_cache"
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
  name         = var.websocket_connections_table
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
    Name = "${var.project_name}-ws-connections"
  }
}
