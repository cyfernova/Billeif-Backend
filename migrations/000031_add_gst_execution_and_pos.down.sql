DROP TABLE IF EXISTS pos_sessions;
DROP TABLE IF EXISTS pos_profiles;
DROP TABLE IF EXISTS gst_submission_attempts;
DROP TABLE IF EXISTS gst_submission_jobs;
DROP TABLE IF EXISTS ewaybill_vehicle_movements;
DROP TABLE IF EXISTS ewaybill_records;
DROP TABLE IF EXISTS einvoice_records;
DROP TABLE IF EXISTS gst_integration_accounts;

ALTER TABLE documents
    DROP COLUMN IF EXISTS current_ewaybill_id,
    DROP COLUMN IF EXISTS current_einvoice_id,
    DROP COLUMN IF EXISTS multi_vehicle_plan,
    DROP COLUMN IF EXISTS vehicle,
    DROP COLUMN IF EXISTS transporter,
    DROP COLUMN IF EXISTS distance_km,
    DROP COLUMN IF EXISTS dispatch_to,
    DROP COLUMN IF EXISTS dispatch_from,
    DROP COLUMN IF EXISTS reverse_charge_reason,
    DROP COLUMN IF EXISTS reverse_charge,
    DROP COLUMN IF EXISTS generate_ewaybill,
    DROP COLUMN IF EXISTS generate_einvoice;
