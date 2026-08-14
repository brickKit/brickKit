-- 0001_init：人员表。
--
-- 迁移文件按文件名顺序执行，执行过的版本记在 schema_migrations 里。
-- 后续结构变更请**新增**文件，不要改这一个（002 §8.9）。

CREATE TABLE people (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    department_id TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_people_department ON people (department_id);
