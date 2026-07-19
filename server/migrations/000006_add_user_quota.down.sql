ALTER TABLE users DROP CONSTRAINT users_quota_used_nonneg;
ALTER TABLE generation_tasks DROP COLUMN quota_charged;
ALTER TABLE users DROP COLUMN quota_used;
ALTER TABLE users DROP COLUMN quota_total;
