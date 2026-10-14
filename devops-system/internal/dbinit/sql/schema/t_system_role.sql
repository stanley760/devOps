-- t_system_role 角色表（租户表）。
-- 复合唯一索引 uk_tenant_code(tenant_id,code) 内联声明。
-- data_scope/data_scope_dept_ids 仅存储，行级数据权限列 Roadmap。
CREATE TABLE IF NOT EXISTS t_system_role (
    id                   BIGINT       NOT NULL,
    creator              VARCHAR(64)  NULL,
    create_time          DATETIME(3)  NULL,
    updater              VARCHAR(64)  NULL,
    update_time          DATETIME(3)  NULL,
    deleted_at           DATETIME(3)  NULL,
    tenant_id            BIGINT       NOT NULL,
    name                 VARCHAR(30)  NOT NULL,
    code                 VARCHAR(100) NOT NULL,
    sort                 BIGINT       NULL,
    data_scope           TINYINT      NOT NULL DEFAULT 1,
    data_scope_dept_ids  VARCHAR(500) NULL,
    status               TINYINT      NOT NULL DEFAULT 1,
    type                 TINYINT      NOT NULL DEFAULT 2,
    remark               VARCHAR(500) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tenant_code (tenant_id, code),
    KEY idx_t_system_role_deleted_at (deleted_at),
    KEY idx_t_system_role_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
