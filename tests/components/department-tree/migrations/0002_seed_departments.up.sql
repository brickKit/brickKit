-- 0002_seed_departments：初始组织架构。
--
-- 这是一个平台自测组件，样例数据是它对外承诺的一部分：
-- people/basic 的样例人员挂在这些部门上，装配起来才有东西可看。
--
-- 用 ON CONFLICT DO UPDATE 而不是纯 INSERT：迁移只会跑一次，
-- 但万一有人手工改过数据、又重建了 schema_migrations，重跑不该炸。

INSERT INTO departments (id, name, parent_id, level) VALUES
    ('d-root',    '总公司',     '',       1),
    ('d-tech',    '技术中心',   'd-root', 2),
    ('d-hr',      '人力资源部', 'd-root', 2),
    ('d-backend', '后端组',     'd-tech', 3)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name, parent_id = EXCLUDED.parent_id, level = EXCLUDED.level;
