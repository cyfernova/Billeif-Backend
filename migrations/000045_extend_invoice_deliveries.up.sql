ALTER TABLE email_deliveries
    DROP CONSTRAINT IF EXISTS email_deliveries_status_check,
    ADD CONSTRAINT email_deliveries_status_check
        CHECK (
            status IN (
                'waiting_for_render',
                'queued',
                'processing',
                'sent',
                'delivered',
                'failed',
                'bounced',
                'complained'
            )
        );

CREATE UNIQUE INDEX idx_email_deliveries_provider_message_unique
    ON email_deliveries (provider_message_id)
    WHERE provider_message_id IS NOT NULL AND provider_message_id <> '';
