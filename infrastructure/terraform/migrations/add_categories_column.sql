ALTER TABLE products ADD COLUMN IF NOT EXISTS categories TEXT[] DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_products_categories ON products USING GIN (categories);