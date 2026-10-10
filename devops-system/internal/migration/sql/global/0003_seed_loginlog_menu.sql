-- 0003 登录日志菜单（t_system_menu 母本增量）：菜单 8 + 按钮 81/82。
-- 独立版本文件而非改 0001：0001 已应用库存量有 checksum 记账，改动会被
-- migration 拒绝启动（防篡改）；增量走新版本号。
-- 曾误用 0002 与 seed_tenant 撞号（load 阶段 fail-closed，从未执行过），改 0003。
-- ID 段沿用母本约定：目录/菜单 1-7，按钮 = 父菜单 ID*10+序号（81/82）。
INSERT INTO t_system_menu
    (id, creator, create_time, updater, update_time,
     name, permission, type, sort, parent_id, path, icon, component,
     status, visible, always_show)
VALUES
    (8,  'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '登录日志', 'system:loginlog:list',   2, 7, 1, 'loginlog', 'ep:document', 'system/loginlog/index', 0, TRUE, TRUE),
    (81, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '日志删除', 'system:loginlog:delete', 3, 1, 8, '', '', '', 0, TRUE, FALSE),
    (82, 'seed', CURRENT_TIMESTAMP, 'seed', CURRENT_TIMESTAMP, '日志清空', 'system:loginlog:clean',  3, 2, 8, '', '', '', 0, TRUE, FALSE)
ON DUPLICATE KEY UPDATE id = id;
