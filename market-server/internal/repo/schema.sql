-- BrickKit Market 库表（007 §10 市场数据模型）
--
-- 迁移是幂等的：市场启动时执行一次即可，重复执行不报错。

CREATE TABLE IF NOT EXISTS components (
    component_id    VARCHAR(256) PRIMARY KEY,
    name            VARCHAR(256) NOT NULL,
    description     TEXT,
    vendor          VARCHAR(256),
    visibility      VARCHAR(32)  NOT NULL DEFAULT 'public',
    source_type     VARCHAR(32)  NOT NULL DEFAULT 'registry',  -- git / registry
    git_url         VARCHAR(512),                              -- 开源组件的 Git 地址
    status          VARCHAR(32)  NOT NULL DEFAULT 'active',
    owner_id        VARCHAR(128) NOT NULL,
    org_id          VARCHAR(128),
    downloads       BIGINT       NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS component_tags (
    component_id    VARCHAR(256) NOT NULL REFERENCES components(component_id) ON DELETE CASCADE,
    tag             VARCHAR(128) NOT NULL,
    PRIMARY KEY (component_id, tag)
);

CREATE TABLE IF NOT EXISTS component_versions (
    component_id    VARCHAR(256) NOT NULL REFERENCES components(component_id) ON DELETE CASCADE,
    version         VARCHAR(64)  NOT NULL,
    status          VARCHAR(32)  NOT NULL DEFAULT 'draft',
    manifest_json   JSONB        NOT NULL,
    changelog       TEXT,
    signature_json  JSONB,
    published_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    published_by    VARCHAR(128),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (component_id, version)
);

-- 产物按 <组件, 版本> 归属，天然做到"每个版本独立存储"（开发计划 18.22）
CREATE TABLE IF NOT EXISTS artifacts (
    component_id    VARCHAR(256) NOT NULL,
    version         VARCHAR(64)  NOT NULL,
    artifact_id     VARCHAR(64)  NOT NULL,
    ordinal         INTEGER      NOT NULL DEFAULT 0,   -- 保持发布时的声明顺序
    type            VARCHAR(64)  NOT NULL,             -- 自由字符串，市场不校验取值
    format          VARCHAR(64),                       -- 自由字符串
    description     TEXT,
    reference       VARCHAR(512),                      -- container 类型的镜像地址
    file_list       JSONB,                             -- 文件路径列表
    uploaded_files  JSONB,                             -- 已上传到对象存储的文件
    checksum        VARCHAR(256),
    size_bytes      BIGINT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (component_id, version, artifact_id),
    FOREIGN KEY (component_id, version)
        REFERENCES component_versions(component_id, version) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS access_policies (
    policy_id       BIGSERIAL PRIMARY KEY,
    component_id    VARCHAR(256) NOT NULL REFERENCES components(component_id) ON DELETE CASCADE,
    target_type     VARCHAR(32)  NOT NULL,  -- user / organization
    target_id       VARCHAR(128) NOT NULL,
    permission      VARCHAR(32)  NOT NULL DEFAULT 'read',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 组织（007 §9.5、§10）。
-- 成员关系记在 users.org_id 上：一个用户至多属于一个组织。
CREATE TABLE IF NOT EXISTS organizations (
    org_id          VARCHAR(128) PRIMARY KEY,
    name            VARCHAR(256) NOT NULL,
    owner_id        VARCHAR(128) NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    user_id         VARCHAR(128) PRIMARY KEY,
    username        VARCHAR(128) NOT NULL UNIQUE,
    email           VARCHAR(256),
    password_hash   VARCHAR(256) NOT NULL,
    org_id          VARCHAR(128),
    is_admin        BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tokens (
    token           VARCHAR(128) PRIMARY KEY,
    user_id         VARCHAR(128) NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    username        VARCHAR(128) NOT NULL,
    expires_at      TIMESTAMPTZ  NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 审计日志只追加，不修改不删除（007 §16.3）
CREATE TABLE IF NOT EXISTS audit_logs (
    audit_id        BIGSERIAL PRIMARY KEY,
    action          VARCHAR(64)  NOT NULL,
    component_id    VARCHAR(256),
    version         VARCHAR(64),
    operator        VARCHAR(128) NOT NULL,
    result          VARCHAR(32)  NOT NULL,
    detail          TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS download_records (
    record_id       BIGSERIAL PRIMARY KEY,
    component_id    VARCHAR(256) NOT NULL,
    version         VARCHAR(64)  NOT NULL,
    artifact_type   VARCHAR(64),
    downloaded_by   VARCHAR(128),
    downloaded_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_components_visibility ON components(visibility);
CREATE INDEX IF NOT EXISTS idx_components_status     ON components(status);
CREATE INDEX IF NOT EXISTS idx_versions_component    ON component_versions(component_id);
CREATE INDEX IF NOT EXISTS idx_audit_component       ON audit_logs(component_id);
CREATE INDEX IF NOT EXISTS idx_audit_action          ON audit_logs(action);
