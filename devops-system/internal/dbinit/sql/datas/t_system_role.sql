-- t_system_role 初始数据：租户 1（default）角色 1=超级管理员（内置）、2=普通角色。
-- 显式 ID + tenant_id。ON DUPLICATE KEY UPDATE id = id 保证重复执行无副作用。
INSERT INTO t_system_role
    (id, tenant_id, creator, create_time, updater, update_time, name, code, sort, data_scope, status, type)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '超级管理员', 'super_admin', 1, 1, 1, 1),
    (2, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '普通角色',   'common',      2, 2, 1, 2)
ON DUPLICATE KEY UPDATE id = id;
