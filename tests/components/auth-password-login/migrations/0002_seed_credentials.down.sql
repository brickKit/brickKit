-- 0002_seed_credentials 的回退：删掉样例账号，保留表结构。
--
-- 真实部署想清掉试用账号时，跑一次 `migrate down 1` 就是这条。

DELETE FROM credentials WHERE username IN ('zhangsan', 'lisi', 'wangwu', 'zhaoliu');
