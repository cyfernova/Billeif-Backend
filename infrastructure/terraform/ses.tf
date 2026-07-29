resource "aws_ses_configuration_set" "main" {
  name = "${local.resource_prefix}-ses-config"

  lifecycle {
    precondition {
      condition = (
        local.ses_verified_identity_is_email ? (
          local.ses_sender_email_local_part == local.ses_verified_identity_local_part &&
          local.ses_sender_email_domain == local.ses_verified_identity_domain
          ) : (
          local.ses_sender_email_domain == local.ses_verified_identity_domain ||
          endswith(local.ses_sender_email_domain, ".${local.ses_verified_identity_domain}")
        )
      )
      error_message = "ses_sender_email must equal the verified email identity or belong to the verified SES domain identity."
    }
  }
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
