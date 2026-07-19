-- 拆分共用的 endpoint 配置项为 dashscope_endpoint / t8star_endpoint 两个独立
-- 键——旧系统里 endpoint 是单一的全局开关，同一时刻只有一个上游生效；
-- 新架构按模型协议选择上游（gpt-image-2 -> t8star，其余 -> dashscope），
-- 两者可能同时配置、同时生效，合用一个 endpoint 会让配置其中一个地址时
-- 连带影响另一个协议的请求。已有的 endpoint 行按旧默认语义（它一直只被
-- dashscope 相关代码路径使用）迁移为 dashscope_endpoint；旧行随 UPDATE
-- 一并消失（key 是主键，这是一次原地改名，不留下旧行）。
UPDATE app_settings SET key = 'dashscope_endpoint' WHERE key = 'endpoint';
