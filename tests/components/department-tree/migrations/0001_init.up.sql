-- 0001_init：部门表。
--
-- 迁移文件按文件名顺序执行，执行过的版本记在 schema_migrations 里，
-- 因此这个文件只会跑一次。后续变更请**新增**文件，不要改这一个
-- （002 §8.9：先兼容后迁移，不做破坏性操作）。

CREATE TABLE departments (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    parent_id  TEXT NOT NULL DEFAULT '',
    level      INT  NOT NULL DEFAULT 1
);

CREATE INDEX idx_departments_parent ON departments (parent_id);
