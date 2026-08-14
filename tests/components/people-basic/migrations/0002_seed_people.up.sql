-- 0002_seed_people：初始人员。
--
-- department_id 对应 department/tree 的 0002_seed_departments.sql：
-- 两个组件的样例数据是对得上的，装配起来才有东西可看。
-- 注意本组件**不存部门名**，那是 department/tree 的数据（002 §2.2 数据自治）。

INSERT INTO people (id, name, department_id, title) VALUES
    ('p-001', '张三', 'd-tech',    '后端工程师'),
    ('p-002', '李四', 'd-tech',    '前端工程师'),
    ('p-003', '王五', 'd-hr',      'HR 专员'),
    ('p-004', '赵六', 'd-backend', '后端工程师')
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    department_id = EXCLUDED.department_id,
    title = EXCLUDED.title;
