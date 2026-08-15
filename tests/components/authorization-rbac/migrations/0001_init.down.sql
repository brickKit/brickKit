-- 0001_init 的回退。
--
-- down 脚本是给**开发与测试**用的（反复搭起来、拆掉）。
-- 生产环境的结构变更请遵循 002 §8.9。

DROP INDEX IF EXISTS idx_role_grants_subject;
DROP TABLE IF EXISTS role_grants;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
