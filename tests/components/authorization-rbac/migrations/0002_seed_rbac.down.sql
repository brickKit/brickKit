-- 0002_seed_rbac 的回退：删掉样例数据，保留表结构。
--
-- 真实部署想清掉样例角色时，跑一次 `migrate down 1` 就是这条。

DELETE FROM role_grants WHERE role_id IN ('r-viewer', 'r-manager', 'r-hr');
DELETE FROM role_permissions WHERE role_id IN ('r-viewer', 'r-manager', 'r-hr');
DELETE FROM roles WHERE id IN ('r-viewer', 'r-manager', 'r-hr');
