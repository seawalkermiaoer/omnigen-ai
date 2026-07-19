-- 反向迁移：把 dashscope_endpoint 改名回 endpoint（原地改名，恢复旧行），
-- 并丢弃 t8star_endpoint——旧 schema 里没有这个键的容身之处。
UPDATE app_settings SET key = 'endpoint' WHERE key = 'dashscope_endpoint';
DELETE FROM app_settings WHERE key = 't8star_endpoint';
