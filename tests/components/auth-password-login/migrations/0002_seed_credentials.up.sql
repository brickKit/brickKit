-- 0002_seed_credentials：初始登录凭据。
--
-- 这是一个平台自测组件，样例数据是它对外承诺的一部分：
-- person_id 对应 people/basic 的样例人员（p-001 ~ p-004），
-- 装配起来才能真的登录一次看看。
--
-- ⚠️ 四个账号的口令都是 demo-password，**只能用于本地试用**。
--    真实部署请先把这些行删掉，或用 migrate down 回退这一版。
--
-- 哈希是 pbkdf2-sha256$<迭代次数>$<盐 base64>$<派生密钥 base64>，每行的盐都不同——
-- 所以四个账号口令相同，哈希却各不一样。这正是加盐要解决的问题：
-- 否则一眼就能看出"这几个人用了同一个密码"。
--
-- 用 ON CONFLICT DO UPDATE 而不是纯 INSERT：迁移只会跑一次，
-- 但万一有人手工改过数据、又重建了 schema_migrations，重跑不该炸。

INSERT INTO credentials (username, person_id, password_hash) VALUES
    ('zhangsan', 'p-001', 'pbkdf2-sha256$600000$/MYQUvnXCdB99P7Vic4nLg$ykletxDDBsQuUGIyo8Xqql+c+meEzs27v8gB85I5k1Y'),
    ('lisi',     'p-002', 'pbkdf2-sha256$600000$wCyiMr2wkw8dDDN+MCeHOg$5hEonx/tIwkAomw1b+1kCtlqJ2njqawXedgicuuXWwk'),
    ('wangwu',   'p-003', 'pbkdf2-sha256$600000$bXQHsJsZr2bHYkK1+N1Gzg$FnT2whISzkn9n4/+NWz59kEEa+jqZ32sy5C/6btMElI'),
    ('zhaoliu',  'p-004', 'pbkdf2-sha256$600000$m8gbJlfJEgvCz0j/DrUniw$hSN78kzrdnKWmf+GE1xsztTlpi8pQGXlboShIuhUivw')
ON CONFLICT (username) DO UPDATE
SET person_id     = EXCLUDED.person_id,
    password_hash = EXCLUDED.password_hash,
    updated_at    = NOW();
