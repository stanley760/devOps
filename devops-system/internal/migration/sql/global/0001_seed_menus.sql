-- 0001 菜单母本（t_system_menu，全局表；模式B 由 provision fan-out 同步到各租户库）。
-- 数据对齐原 Go 种子 buildSeedMenus：显式 ID、不走发号；按钮 always_show=FALSE。
-- ON DUPLICATE KEY UPDATE 兼容「已跑过旧 Go 种子」的存量库：版本表为空但
-- 菜单已存在时首轮按 no-op 补记账，此后由版本表跳过（MySQL 方言）。
INSERT INTO t_system_menu
    (id, creator, create_time, updater, update_time,
     name, permission, type, sort, parent_id, path, icon, component,
     status, visible, always_show)
VALUES
    (1,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '系统管理', '', 1, 1, 0, '/system', 'ep:setting', '', 0, TRUE, TRUE),
    (2,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '用户管理', 'system:user:list',   2, 1, 1, 'user',  'ep:user',            'system/user/index',  0, TRUE, TRUE),
    (3,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '角色管理', 'system:role:list',   2, 2, 1, 'role',  'ep:key',             'system/role/index',  0, TRUE, TRUE),
    (4,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '菜单管理', 'system:menu:list',   2, 3, 1, 'menu',  'ep:menu',            'system/menu/index',  0, TRUE, TRUE),
    (5,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '部门管理', 'system:dept:list',   2, 4, 1, 'dept',  'ep:office-building', 'system/dept/index',  0, TRUE, TRUE),
    (6,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '岗位管理', 'system:post:list',   2, 5, 1, 'post',  'ep:postcard',        'system/post/index',  0, TRUE, TRUE),
    (7,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '租户管理', '',                   2, 6, 1, 'tenant', 'ep:coin',            'platform/tenant/index', 0, TRUE, TRUE),
    (21, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '用户查询', 'system:user:query',          3, 1, 2, '', '', '', 0, TRUE, FALSE),
    (22, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '用户新增', 'system:user:create',         3, 2, 2, '', '', '', 0, TRUE, FALSE),
    (23, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '用户修改', 'system:user:update',         3, 3, 2, '', '', '', 0, TRUE, FALSE),
    (24, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '用户删除', 'system:user:delete',         3, 4, 2, '', '', '', 0, TRUE, FALSE),
    (25, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '重置密码', 'system:user:update-password', 3, 5, 2, '', '', '', 0, TRUE, FALSE),
    (31, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '角色查询', 'system:role:query',          3, 1, 3, '', '', '', 0, TRUE, FALSE),
    (32, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '角色新增', 'system:role:create',        3, 2, 3, '', '', '', 0, TRUE, FALSE),
    (33, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '角色修改', 'system:role:update',         3, 3, 3, '', '', '', 0, TRUE, FALSE),
    (34, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '角色删除', 'system:role:delete',         3, 4, 3, '', '', '', 0, TRUE, FALSE),
    (41, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '菜单查询', 'system:menu:query',          3, 1, 4, '', '', '', 0, TRUE, FALSE),
    (42, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '菜单新增', 'system:menu:create',         3, 2, 4, '', '', '', 0, TRUE, FALSE),
    (43, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '菜单修改', 'system:menu:update',         3, 3, 4, '', '', '', 0, TRUE, FALSE),
    (44, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '菜单删除', 'system:menu:delete',         3, 4, 4, '', '', '', 0, TRUE, FALSE),
    (51, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '部门查询', 'system:dept:query',          3, 1, 5, '', '', '', 0, TRUE, FALSE),
    (52, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '部门新增', 'system:dept:create',         3, 2, 5, '', '', '', 0, TRUE, FALSE),
    (53, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '部门修改', 'system:dept:update',         3, 3, 5, '', '', '', 0, TRUE, FALSE),
    (54, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '部门删除', 'system:dept:delete',         3, 4, 5, '', '', '', 0, TRUE, FALSE),
    (61, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '岗位查询', 'system:post:query',          3, 1, 6, '', '', '', 0, TRUE, FALSE),
    (62, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '岗位新增', 'system:post:create',         3, 2, 6, '', '', '', 0, TRUE, FALSE),
    (63, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '岗位修改', 'system:post:update',         3, 3, 6, '', '', '', 0, TRUE, FALSE),
    (64, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '岗位删除', 'system:post:delete',         3, 4, 6, '', '', '', 0, TRUE, FALSE)
ON DUPLICATE KEY UPDATE id = id;
