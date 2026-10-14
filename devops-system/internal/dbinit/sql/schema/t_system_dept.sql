-- t_system_dept 部门表（租户表）。树结构内存组装（无 ancestors 列，对齐 yudao）。
CREATE TABLE IF NOT EXISTS t_system_dept (
    id             BIGINT       NOT NULL,
    creator        VARCHAR(64)  NULL,
    create_time    DATETIME(3)  NULL,
    updater        VARCHAR(64)  NULL,
    update_time    DATETIME(3)  NULL,
    deleted_at     DATETIME(3)  NULL,
    tenant_id      BIGINT       NOT NULL,
    name           VARCHAR(30)  NOT NULL,
    parent_id      BIGINT       NULL DEFAULT 0,
    sort           BIGINT       NULL,
    leader_user_id BIGINT       NULL,
    phone          VARCHAR(20)  NULL,
    email          VARCHAR(50)  NULL,
    status         TINYINT      NOT NULL DEFAULT 1,
    PRIMARY KEY (id),
    KEY idx_t_system_dept_deleted_at (deleted_at),
    KEY idx_t_system_dept_tenant_id (tenant_id),
    KEY idx_t_system_dept_parent_id (parent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
