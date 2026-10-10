-- 0002 默认租户注册行（t_tenant，全局表）：tenant default(1)。
-- ON DUPLICATE KEY UPDATE 兼容存量库补记账（MySQL 方言）。
INSERT INTO t_tenant (id, creator, create_time, updater, update_time, name, code, status)
VALUES (1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'devOps', 'default', 0)
ON DUPLICATE KEY UPDATE id = id;
