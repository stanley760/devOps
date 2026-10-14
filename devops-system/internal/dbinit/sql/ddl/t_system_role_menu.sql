-- t_system_role_menu 角色-菜单关联（租户表；菜单全局，但授权按租户的角色发生）。
-- 复合主键、无独立 ID、不走发号；物理删除（无 deleted_at），理由同 t_system_user_role。
CREATE TABLE IF NOT EXISTS t_system_role_menu (
    tenant_id BIGINT NOT NULL,
    role_id   BIGINT NOT NULL,
    menu_id   BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, menu_id),
    KEY idx_role_menu_role (role_id),
    KEY idx_role_menu_menu (menu_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
