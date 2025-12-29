resource "aws_ses_email_identity" "main" {
  email = "noreply@invoice-platform.local"
}

resource "aws_ses_configuration_set" "main" {
  name = "invoice-platform-config"
}

resource "aws_ses_event_destination" "to_s3" {
  name                   = "SendToS3"
  configuration_set_name = aws_ses_configuration_set.main.name
  matching_types         = ["SEND"]

  s3_destination {
    bucket_name = aws_s3_bucket.email_sink.id
    position    = "BEFORE"
  }
}
