-- 003_verified_links.sql
-- Allow users reporting real rent to attach an official building website or listing URL,
-- and expand max valid rent ceiling to support luxury serviced residences (up to 500,000 THB/mo).

ALTER TABLE price_reports
    ADD COLUMN IF NOT EXISTS source_url TEXT NOT NULL DEFAULT '';

ALTER TABLE price_reports DROP CONSTRAINT IF EXISTS price_reports_source_url_check;
ALTER TABLE price_reports
    ADD CONSTRAINT price_reports_source_url_check
    CHECK (char_length(source_url) <= 500);

ALTER TABLE price_reports DROP CONSTRAINT IF EXISTS price_reports_monthly_rent_thb_check;
ALTER TABLE price_reports
    ADD CONSTRAINT price_reports_monthly_rent_thb_check
    CHECK (monthly_rent_thb BETWEEN 1500 AND 500000);
