-- 0001_init：登录凭据表。
--
-- 迁移文件按文件名顺序执行，执行过的版本记在 schema_migrations 里，
-- 因此这个文件只会跑一次。后续变更请**新增**文件，不要改这一个
-- （002 §8.9：先兼容后迁移，不做破坏性操作）。
--
-- 表里只有"怎么证明你是你"，没有"你是谁"：姓名、部门在 people/basic。
-- person_id 是跨组件的引用，**不加外键**——那是另一个组件的另一个库。

CREATE TABLE credentials (
    username      TEXT PRIMARY KEY,
    person_id     TEXT NOT NULL,
    -- 只存哈希。明文口令在本组件的任何地方都不落地
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 一个人可以有多个登录名（工号、邮箱），所以这里不是唯一索引
CREATE INDEX idx_credentials_person ON credentials (person_id);
