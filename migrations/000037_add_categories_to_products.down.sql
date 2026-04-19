-- Remove categories column from products table
ALTER TABLE products DROP COLUMN IF EXISTS categories;
