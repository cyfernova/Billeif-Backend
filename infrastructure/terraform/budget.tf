locals {
  monthly_budget_notifications = {
    actual_50 = {
      notification_type = "ACTUAL"
      threshold         = 50
    }
    actual_80 = {
      notification_type = "ACTUAL"
      threshold         = 80
    }
    actual_100 = {
      notification_type = "ACTUAL"
      threshold         = 100
    }
    forecasted_100 = {
      notification_type = "FORECASTED"
      threshold         = 100
    }
  }
}

resource "aws_budgets_budget" "monthly_cost" {
  name         = "${local.resource_prefix}-monthly-cost"
  budget_type  = "COST"
  limit_amount = "125"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  dynamic "notification" {
    for_each = var.alert_email != "" ? local.monthly_budget_notifications : {}

    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value.threshold
      threshold_type             = "PERCENTAGE"
      notification_type          = notification.value.notification_type
      subscriber_email_addresses = [var.alert_email]
    }
  }
}
