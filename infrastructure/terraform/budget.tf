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

  lifecycle {
    precondition {
      condition     = !var.enable_application || trimspace(var.alert_email) != ""
      error_message = "alert_email must be supplied before enabling the Billeif application so cost and operational alerts have a recipient."
    }

    precondition {
      condition     = !var.enable_application || var.alert_email_subscription_confirmed
      error_message = "alert_email_subscription_confirmed must be true before enabling the Billeif application; confirm the SNS email subscription first."
    }
  }
}
