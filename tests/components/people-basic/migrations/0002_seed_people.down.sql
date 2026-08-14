-- 0002_seed_people 的回退：删掉初始数据，保留表结构。

DELETE FROM people WHERE id IN ('p-001', 'p-002', 'p-003', 'p-004');
