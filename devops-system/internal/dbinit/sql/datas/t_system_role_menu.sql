-- t_system_role_menu 初始数据：租户 1 super_admin(1) → 全量菜单授权（34 条）。
-- 菜单可见性走 role_menu；casbin p 行为域内通配（super_admin, 1, *），
-- 新权限无需补策略。ON DUPLICATE KEY UPDATE tenant_id = tenant_id 保证重复执行无副作用。
INSERT INTO t_system_role_menu (tenant_id, role_id, menu_id)
VALUES
    (1, 1, 1), (1, 1, 2), (1, 1, 3), (1, 1, 4), (1, 1, 5), (1, 1, 6), (1, 1, 7),
    (1, 1, 8), (1, 1, 9),
    (1, 1, 21), (1, 1, 22), (1, 1, 23), (1, 1, 24), (1, 1, 25),
    (1, 1, 31), (1, 1, 32), (1, 1, 33), (1, 1, 34),
    (1, 1, 41), (1, 1, 42), (1, 1, 43), (1, 1, 44),
    (1, 1, 51), (1, 1, 52), (1, 1, 53), (1, 1, 54),
    (1, 1, 61), (1, 1, 62), (1, 1, 63), (1, 1, 64),
    (1, 1, 81), (1, 1, 82),
    (1, 1, 91), (1, 1, 92)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;
