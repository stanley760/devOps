-- 0003 操作日志菜单授权：租户 1 super_admin 补授菜单 9/91/92
-- （菜单可见性走 role_menu；casbin p 行为域内通配（super_admin, 1, *），
--  新权限无需补策略）。
INSERT INTO t_system_role_menu (tenant_id, role_id, menu_id)
VALUES
    (1, 1, 9),
    (1, 1, 91),
    (1, 1, 92)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;