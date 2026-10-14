-- t_system_user_role 用户-角色关联（租户表）。
-- 复合主键、无独立 ID、不走发号；物理删除（无 deleted_at）——
-- 分配/取消分配是幂等操作，软删会使复合主键无法重建同组合行。
CREATE TABLE IF NOT EXISTS t_system_user_role (
    tenant_id BIGINT NOT NULL,
    user_id   BIGINT NOT NULL,
    role_id   BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, user_id, role_id),
    KEY idx_user_role_user (user_id),
    KEY idx_user_role_role (role_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
