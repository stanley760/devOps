-- t_tenant 租户注册表（全局表，无 tenant_id）：code 全局唯一，前端租户标识。
CREATE TABLE IF NOT EXISTS t_tenant (
    id             BIGINT       NOT NULL,
    creator        VARCHAR(64)  NULL,
    create_time    DATETIME(3)  NULL,
    updater        VARCHAR(64)  NULL,
    update_time    DATETIME(3)  NULL,
    deleted_at     DATETIME(3)  NULL,
    name           VARCHAR(30)  NOT NULL,
    code           VARCHAR(30)  NOT NULL,
    contact_name   VARCHAR(30)  NULL,
    contact_mobile VARCHAR(500) NULL,
    status         TINYINT      NOT NULL DEFAULT 1,
    expire_time    DATETIME(3)  NULL,
    remark         VARCHAR(500) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_code (code),
    KEY idx_t_tenant_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
