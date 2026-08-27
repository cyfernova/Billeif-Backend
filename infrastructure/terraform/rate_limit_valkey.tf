locals {
  rate_limit_cache_name = "${local.resource_prefix}-rate-limit"
  rate_limit_user_id    = "${local.resource_prefix}-http"

  rate_limit_http_env = {
    REDIS_HOST                  = aws_elasticache_serverless_cache.rate_limit.endpoint[0].address
    REDIS_PORT                  = tostring(aws_elasticache_serverless_cache.rate_limit.endpoint[0].port)
    REDIS_USER_ID               = aws_elasticache_user.rate_limit_http.user_id
    REDIS_CACHE_NAME            = aws_elasticache_serverless_cache.rate_limit.name
    REDIS_TLS_ENABLED           = "true"
    REDIS_IAM_AUTH_ENABLED      = "true"
    REDIS_CLUSTER_MODE          = "true"
    RATE_LIMIT_DECISION_TIMEOUT = "250ms"
  }
}

resource "aws_security_group" "rate_limit_valkey" {
  name        = "${local.resource_prefix}-rate-limit-valkey-sg"
  description = "${local.resource_prefix} Valkey rate-limit cache from HTTP Lambda"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "TLS Valkey from Billeif Lambda"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  tags = {
    Name = "${local.resource_prefix}-rate-limit-valkey-sg"
  }
}

resource "aws_elasticache_user" "rate_limit_http" {
  user_id       = local.rate_limit_user_id
  user_name     = local.rate_limit_user_id
  engine        = "valkey"
  access_string = "on ~{billeif-rate-limit}:* -@all +@connection +cluster|slots +get +incr +pexpire +pttl +eval +evalsha"

  authentication_mode {
    type = "iam"
  }
}

resource "aws_elasticache_user_group" "rate_limit" {
  user_group_id = "${local.resource_prefix}-rate-limit"
  engine        = "valkey"
  user_ids      = [aws_elasticache_user.rate_limit_http.user_id]
}

resource "aws_elasticache_serverless_cache" "rate_limit" {
  name                 = local.rate_limit_cache_name
  description          = "${local.resource_prefix} distributed HTTP rate-limit decisions"
  engine               = "valkey"
  major_engine_version = "7"
  security_group_ids   = [aws_security_group.rate_limit_valkey.id]
  subnet_ids           = aws_subnet.private[*].id
  user_group_id        = aws_elasticache_user_group.rate_limit.user_group_id

  cache_usage_limits {
    data_storage {
      maximum = 1
      unit    = "GB"
    }

    ecpu_per_second {
      maximum = 1000
    }
  }
}

resource "aws_iam_role_policy" "rate_limit_connect" {
  name = "${local.resource_prefix}-rate-limit-connect"
  role = aws_iam_role.lambda_http_exec.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "ConnectToRateLimitValkey"
      Effect = "Allow"
      Action = ["elasticache:Connect"]
      Resource = [
        aws_elasticache_serverless_cache.rate_limit.arn,
        aws_elasticache_user.rate_limit_http.arn,
      ]
      Condition = {
        StringEquals = {
          "aws:SourceVpc" = aws_vpc.main.id
        }
      }
    }]
  })
}
