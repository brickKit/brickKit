-- 0001_init：角色、权限与授权三张表。
--
-- 迁移文件按文件名顺序执行，执行过的版本记在 schema_migrations 里，
-- 因此这个文件只会跑一次。后续变更请**新增**文件，不要改这一个
-- （002 §8.9：先兼容后迁移，不做破坏性操作）。

CREATE TABLE roles (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

-- 权限是自由字符串（如 erp.order.read）。本组件不理解它的含义，
-- 只负责"谁有哪些"——权限名的语义由使用它的业务组件自己定义
CREATE TABLE role_permissions (
    role_id    TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role_id, permission)
);

-- 授权：把角色授予「人」或「部门」。
--
-- subject_id 是跨组件的引用（personId 来自 people/basic、departmentId 来自
-- department/tree），**不加外键**——那是别的组件的别的库。
CREATE TABLE role_grants (
    role_id      TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('person', 'department')),
    subject_id   TEXT NOT NULL,
    PRIMARY KEY (role_id, subject_type, subject_id)
);

-- 查询总是按 (subject_type, subject_id) 走，这个索引正好覆盖
CREATE INDEX idx_role_grants_subject ON role_grants (subject_type, subject_id);
