resource "aws_ses_email_identity" "main" {
  email = "noreply@billeif.local"
}

resource "aws_ses_configuration_set" "main" {
  name = "${local.resource_name_prefix}-email"
}

resource "aws_sns_topic" "ses_events" {
  name = "${local.resource_name_prefix}-ses-events"
}

resource "aws_ses_event_destination" "to_sns" {
  name                   = "${local.resource_name_prefix}-send-to-sns"
  configuration_set_name = aws_ses_configuration_set.main.name
  enabled                = true
  matching_types         = ["send", "bounce", "complaint", "delivery"]

  sns_destination {
    topic_arn = aws_sns_topic.ses_events.arn
  }
}
