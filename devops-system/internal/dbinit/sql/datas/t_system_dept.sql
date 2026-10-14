-- t_system_dept 初始数据：租户 1（default）部门 100=devops(root) → 101 技术部、102 运维部。
-- 显式 ID + tenant_id。ON DUPLICATE KEY UPDATE id = id 保证重复执行无副作用。
INSERT INTO t_system_dept
    (id, tenant_id, creator, create_time, updater, update_time, name, parent_id, sort, status)
VALUES
    (100, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'devops', 0,   0, 1),
    (101, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '技术部', 100,  1, 1),
    (102, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '运维部', 100,  2, 1)
ON DUPLICATE KEY UPDATE id = id;
