-- 0002_seed_departments 的回退：删掉初始数据，保留表结构。

DELETE FROM departments WHERE id IN ('d-root', 'd-tech', 'd-hr', 'd-backend');
