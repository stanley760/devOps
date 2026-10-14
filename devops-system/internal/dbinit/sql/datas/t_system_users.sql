-- t_system_users 初始数据：租户 1（default）管理员 1=admin / Admin@123
-- （dept 101，post_ids [4]）。密码为 bcrypt(cost=10) 固化哈希（种子即代码）。
-- 显式 ID + tenant_id。ON DUPLICATE KEY UPDATE id = id 保证重复执行无副作用。
INSERT INTO t_system_users
    (id, tenant_id, creator, create_time, updater, update_time,
     username, password, nickname, dept_id, post_ids, email, gender, status)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP,
     'admin', '$2a$10$r0/5Jgky6pc3mBLuC6.Xs.3Ysdo8mu04VWehEm15I/qIbGQ4IaBXq', 'admin', 101, '[4]', 'admin@devops.local', 0, 1)
ON DUPLICATE KEY UPDATE id = id;
