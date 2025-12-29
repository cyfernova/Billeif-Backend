resource "aws_ses_email_identity" "main" {
  email = "noreply@invoice-platform.local"
}

resource "aws_ses_configuration_set" "main" {
  name = "invoice-platform-config"
}

resource "aws_sns_topic" "ses_events" {
  name = "ses-email-events"
}

resource "aws_ses_event_destination" "to_sns" {
  name                   = "SendToSNS"
  configuration_set_name = aws_ses_configuration_set.main.name
  enabled                = true
  matching_types         = ["send", "bounce", "complaint", "delivery"]

  sns_destination {
    topic_arn = aws_sns_topic.ses_events.arn
  }
}
