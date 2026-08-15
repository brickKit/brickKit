-- 0002_seed_rbac：初始角色与授权。
--
-- 这是一个平台自测组件，样例数据是它对外承诺的一部分：
-- subject_id 对应 people/basic 的样例人员（p-001~p-004）与
-- department/tree 的样例部门（d-tech / d-hr），装配起来才有东西可看。
--
-- 特意安排了三种情形，好让"权限从哪来"这件事看得见：
--   p-001  既有直接授权（r-viewer），又通过部门 d-tech 拿到 r-manager
--   p-002  只通过部门 d-tech 拿到权限
--   p-003  在 d-hr，只有部门授权 r-hr
--   p-004  什么都没有 —— 空权限也要能正确表达

INSERT INTO roles (id, name) VALUES
    ('r-viewer',  '查看者'),
    ('r-manager', '主管'),
    ('r-hr',      '人事')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;

INSERT INTO role_permissions (role_id, permission) VALUES
    ('r-viewer',  'erp.order.read'),
    ('r-manager', 'erp.order.read'),
    ('r-manager', 'erp.order.approve'),
    ('r-hr',      'people.person.read')
ON CONFLICT DO NOTHING;

INSERT INTO role_grants (role_id, subject_type, subject_id) VALUES
    ('r-viewer',  'person',     'p-001'),
    ('r-manager', 'department', 'd-tech'),
    ('r-hr',      'department', 'd-hr')
ON CONFLICT DO NOTHING;
