-- Add profile_picture_url column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS profile_picture_url VARCHAR(2048);
