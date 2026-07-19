-- quota_total 为 NULL 表示不限量。刻意不用 -1 之类的魔法值：
-- SQL 里 "quota_total IS NULL OR quota_used < quota_total" 读起来就是它的字面意思。
ALTER TABLE users
  ADD COLUMN quota_total INT,
  ADD COLUMN quota_used  INT NOT NULL DEFAULT 0;

-- quota_charged 用于防重复退款。视频异步，轮询 worker 判失败时退回；
-- worker 若重跑同一任务，没有这个标志就会退第二次，凭空送额度。
ALTER TABLE generation_tasks
  ADD COLUMN quota_charged BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE users ADD CONSTRAINT users_quota_used_nonneg CHECK (quota_used >= 0);
