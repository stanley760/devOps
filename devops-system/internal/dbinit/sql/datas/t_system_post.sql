-- t_system_post 初始数据：租户 1（default）岗位 1=ceo、2=se、3=ops、4=user。
-- 显式 ID + tenant_id。ON DUPLICATE KEY UPDATE id = id 保证重复执行无副作用。
INSERT INTO t_system_post
    (id, tenant_id, creator, create_time, updater, update_time, code, name, sort, status)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'ceo',  '董事长',     1, 1),
    (2, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'se',   '架构师',     2, 1),
    (3, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'ops',  '运维工程师', 3, 1),
    (4, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'user', '普通用户',   4, 1)
ON DUPLICATE KEY UPDATE id = id;
