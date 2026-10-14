-- t_system_user_role 初始数据：admin(1) → super_admin(1)。
-- ON DUPLICATE KEY UPDATE tenant_id = tenant_id 保证重复执行无副作用。
INSERT INTO t_system_user_role (tenant_id, user_id, role_id)
VALUES (1, 1, 1)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;
