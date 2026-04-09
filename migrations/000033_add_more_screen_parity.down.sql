DROP INDEX IF EXISTS idx_email_deliveries_account_created;
DROP INDEX IF EXISTS idx_email_deliveries_business_created;
DROP TABLE IF EXISTS email_deliveries;

DROP INDEX IF EXISTS idx_email_accounts_business_email;
DROP TABLE IF EXISTS email_accounts;

ALTER TABLE drive_assets
    DROP COLUMN IF EXISTS folder_path;
