DROP INDEX IF EXISTS idx_email_deliveries_provider_message_unique;

ALTER TABLE email_deliveries
    DROP CONSTRAINT IF EXISTS email_deliveries_status_check;

UPDATE email_deliveries
SET status = CASE
    WHEN status = 'waiting_for_render' THEN 'failed'
    WHEN status IN ('bounced', 'complained') THEN 'failed'
    ELSE status
END
WHERE status IN ('waiting_for_render', 'bounced', 'complained');

ALTER TABLE email_deliveries
    ADD CONSTRAINT email_deliveries_status_check
        CHECK (status IN ('queued', 'processing', 'sent', 'delivered', 'failed'));
