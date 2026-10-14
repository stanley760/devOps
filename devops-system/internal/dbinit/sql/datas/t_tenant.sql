-- t_tenant 初始数据：默认租户 tenant default(1)。
-- 显式 ID、不走发号。ON DUPLICATE KEY UPDATE id = id 保证重复执行无副作用。
INSERT INTO t_tenant (id, creator, create_time, updater, update_time, name, code, status)
VALUES (1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'devops', 'default', 1)
ON DUPLICATE KEY UPDATE id = id;
