-- 0001_init 的回退。
--
-- down 脚本是给**开发与测试**用的（反复搭起来、拆掉）。
-- 生产环境的结构变更请遵循 002 §8.9：先兼容后迁移、不做破坏性操作，
-- 出问题应当用一个新的 up 迁移来修，而不是 down 回去。

DROP INDEX IF EXISTS idx_departments_parent;
DROP TABLE IF EXISTS departments;
