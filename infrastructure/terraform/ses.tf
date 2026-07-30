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

data "aws_iam_policy_document" "ses_events" {
  statement {
    sid       = "AllowSESEventPublishing"
    effect    = "Allow"
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.ses_events.arn]

    principals {
      type        = "Service"
      identifiers = ["ses.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }

    condition {
      test     = "ArnEquals"
      variable = "AWS:SourceArn"
      values = [
        "arn:${data.aws_partition.current.partition}:ses:${var.aws_region}:${data.aws_caller_identity.current.account_id}:configuration-set/${aws_ses_configuration_set.main.name}",
      ]
    }
  }
}

resource "aws_sns_topic_policy" "ses_events" {
  arn    = aws_sns_topic.ses_events.arn
  policy = data.aws_iam_policy_document.ses_events.json
}

resource "aws_ses_event_destination" "to_sns" {
  name                   = "${local.resource_prefix}-ses-events"
  configuration_set_name = aws_ses_configuration_set.main.name
  enabled                = true
  matching_types         = ["bounce", "complaint", "delivery"]

  sns_destination {
    topic_arn = aws_sns_topic.ses_events.arn
  }

  depends_on = [aws_sns_topic_policy.ses_events]
}
