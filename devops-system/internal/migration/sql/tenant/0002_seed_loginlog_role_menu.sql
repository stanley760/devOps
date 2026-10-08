-- 0002 登录日志菜单授权：租户 1 super_admin 补授菜单 8/81/82（菜单可见性走
-- role_menu；casbin p 行为域内通配（super_admin, 1, *），新权限无需补策略）。
INSERT INTO t_system_role_menu (tenant_id, role_id, menu_id)
VALUES
    (1, 1, 8),
    (1, 1, 81),
    (1, 1, 82)
ON DUPLICATE KEY UPDATE tenant_id = tenant_id;
