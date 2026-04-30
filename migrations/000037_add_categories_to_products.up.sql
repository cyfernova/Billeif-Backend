-- Add categories column to products table (array of category names)
ALTER TABLE products ADD COLUMN IF NOT EXISTS categories TEXT[] DEFAULT '{}';

-- Create index for faster category queries
CREATE INDEX IF NOT EXISTS idx_products_categories ON products USING GIN (categories);
