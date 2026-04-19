-- Add categories column to products table (array of category names)
ALTER TABLE products ADD COLUMN categories TEXT[] DEFAULT '{}';

-- Create index for faster category queries
CREATE INDEX idx_products_categories ON products USING GIN (categories);
