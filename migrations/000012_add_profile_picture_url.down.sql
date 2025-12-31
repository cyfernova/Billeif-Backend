-- Remove profile_picture_url column from users table
ALTER TABLE users DROP COLUMN IF EXISTS profile_picture_url;
