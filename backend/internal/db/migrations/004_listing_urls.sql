-- 004_listing_urls.sql
-- Store verified direct building rental URLs (PropertyHub project, RentHub apartment, official residence site)
-- and verified station-level portal URLs for fallback direct rental browsing.

ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS listing_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS listing_url_label TEXT NOT NULL DEFAULT '';

ALTER TABLE listings DROP CONSTRAINT IF EXISTS listings_listing_url_check;
ALTER TABLE listings
    ADD CONSTRAINT listings_listing_url_check
    CHECK (char_length(listing_url) <= 500);

ALTER TABLE listings DROP CONSTRAINT IF EXISTS listings_listing_url_label_check;
ALTER TABLE listings
    ADD CONSTRAINT listings_listing_url_label_check
    CHECK (char_length(listing_url_label) <= 60);

ALTER TABLE stations
    ADD COLUMN IF NOT EXISTS portal_condo_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS portal_apt_url TEXT NOT NULL DEFAULT '';

ALTER TABLE stations DROP CONSTRAINT IF EXISTS stations_portal_condo_url_check;
ALTER TABLE stations
    ADD CONSTRAINT stations_portal_condo_url_check
    CHECK (char_length(portal_condo_url) <= 500);

ALTER TABLE stations DROP CONSTRAINT IF EXISTS stations_portal_apt_url_check;
ALTER TABLE stations
    ADD CONSTRAINT stations_portal_apt_url_check
    CHECK (char_length(portal_apt_url) <= 500);
