-- 0004 操作日志菜单（t_system_menu 母本增量）：菜单 9 + 按钮 91/92/93。
-- 独立版本文件而非改0001/0003：已应用库存量有 checksum 记账，改动会被
-- migration 拒绝启动（防篡改）；增量走新版本号。
-- ID 段沿用母本约定：目录/菜单 1-8，按钮 = 父菜单 ID*10+序号（91/92/93）。
-- 与登录日志（菜单 8）分工：登录日志记录认证链路成败，操作日志记录 B 端
-- 管理写操作（用户/角色/菜单/部门/岗位/租户/个人账号）。
INSERT INTO t_system_menu
    (id, creator, create_time, updater, update_time,
     name, permission, type, sort, parent_id, path, icon, component,
     status, visible, always_show)
VALUES
    (9,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '操作日志', 'system:operationlog:list',   2, 8, 1, 'operationlog', 'ep:document', 'system/operationlog/index', 1, TRUE, TRUE),
    (91, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '日志删除', 'system:operationlog:delete', 3, 1, 9, '', '', '', 1, TRUE, FALSE),
    (92, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '日志清空', 'system:operationlog:clean',  3, 2, 9, '', '', '', 1, TRUE, FALSE)
ON DUPLICATE KEY UPDATE id = id;