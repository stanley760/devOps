-- 0001 租户 1（default）业务种子：部门 / 岗位 / 角色 / 用户 admin / 授权。
-- 显式 ID + tenant_id，与原 Go 种子 seedTenantData 完全对齐；
-- 密码 Admin@123 的 bcrypt(cost=10) 固化哈希（种子即代码，与原 Go 硬编码同级）。
-- 落点：shared 模式 = 主库；模式B = tenant_default 库（版本表随库记账）。
-- ON DUPLICATE KEY UPDATE 兼容存量库补记账（MySQL 方言）。

-- 部门：100=devOps(root) → 101 技术部、102 运维部
INSERT INTO t_system_dept
    (id, tenant_id, creator, create_time, updater, update_time, name, parent_id, sort, status)
VALUES
    (100, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'devOps', 0,   0, 0),
    (101, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '技术部', 100,  1, 0),
    (102, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '运维部', 100,  2, 0)
ON DUPLICATE KEY UPDATE id = id;

-- 岗位：1=ceo、2=se、3=ops、4=user
INSERT INTO t_system_post
    (id, tenant_id, creator, create_time, updater, update_time, code, name, sort, status)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'ceo',  '董事长',     1, 0),
    (2, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'se',   '架构师',     2, 0),
    (3, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'ops',  '运维工程师', 3, 0),
    (4, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, 'user', '普通用户',   4, 0)
ON DUPLICATE KEY UPDATE id = id;

-- 角色：1=超级管理员（内置）、2=普通角色
INSERT INTO t_system_role
    (id, tenant_id, creator, create_time, updater, update_time, name, code, sort, data_scope, status, type)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '超级管理员', 'super_admin', 1, 1, 0, 1),
    (2, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '普通角色',   'common',      2, 2, 0, 2)
ON DUPLICATE KEY UPDATE id = id;

-- 用户：1=admin / Admin@123（dept 101，post_ids [4]）
INSERT INTO t_system_users
    (id, tenant_id, creator, create_time, updater, update_time,
     username, password, nickname, dept_id, post_ids, email, sex, status)
VALUES
    (1, 1, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP,
     'admin', '$2a$10$r0/5Jgky6pc3mBLuC6.Xs.3Ysdo8mu04VWehEm15I/qIbGQ4IaBXq', 'admin', 101, '[4]', 'admin@devops.local', 0, 0)
ON DUPLICATE KEY UPDATE id = id;

-- 授权：admin(1) → super_admin(1)
INSERT INTO t_system_user_role (tenant_id, user_id, role_id)
VALUES (1, 1, 1)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;

-- 授权：super_admin(1) → 全量菜单（含菜单 7 租户管理）
INSERT INTO t_system_role_menu (tenant_id, role_id, menu_id)
VALUES
    (1, 1, 1), (1, 1, 2), (1, 1, 3), (1, 1, 4), (1, 1, 5), (1, 1, 6), (1, 1, 7),
    (1, 1, 21), (1, 1, 22), (1, 1, 23), (1, 1, 24), (1, 1, 25),
    (1, 1, 31), (1, 1, 32), (1, 1, 33), (1, 1, 34),
    (1, 1, 41), (1, 1, 42), (1, 1, 43), (1, 1, 44),
    (1, 1, 51), (1, 1, 52), (1, 1, 53), (1, 1, 54),
    (1, 1, 61), (1, 1, 62), (1, 1, 63), (1, 1, 64)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;
