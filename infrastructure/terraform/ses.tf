resource "aws_ses_email_identity" "main" {
  email = var.ses_verified_sender
}

resource "aws_ses_configuration_set" "main" {
  name = "${local.resource_prefix}-ses-config"
}

resource "aws_sns_topic" "ses_events" {
  name = "${local.resource_prefix}-ses-email-events"
}

resource "aws_ses_event_destination" "to_sns" {
  name                   = "${local.resource_prefix}-ses-events"
  configuration_set_name = aws_ses_configuration_set.main.name
  enabled                = true
  matching_types         = ["send", "bounce", "complaint", "delivery"]

  sns_destination {
    topic_arn = aws_sns_topic.ses_events.arn
  }
}
