-- t_system_post 岗位表（租户表）。
-- 复合唯一索引 uk_tenant_code(tenant_id,code) 内联声明。
CREATE TABLE IF NOT EXISTS t_system_post (
    id          BIGINT       NOT NULL,
    creator     VARCHAR(64)  NULL,
    create_time DATETIME(3)  NULL,
    updater     VARCHAR(64)  NULL,
    update_time DATETIME(3)  NULL,
    deleted_at  DATETIME(3)  NULL,
    tenant_id   BIGINT       NOT NULL,
    code        VARCHAR(64)  NOT NULL,
    name        VARCHAR(50)  NOT NULL,
    sort        BIGINT       NULL,
    status      TINYINT      NOT NULL DEFAULT 1,
    remark      VARCHAR(500) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tenant_code (tenant_id, code),
    KEY idx_t_system_post_deleted_at (deleted_at),
    KEY idx_t_system_post_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
