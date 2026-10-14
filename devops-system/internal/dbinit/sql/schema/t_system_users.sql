-- t_system_users 用户表（租户表）。
-- 复合唯一索引跨 tenant_id 与业务列，内联声明：
-- uk_tenant_username(tenant_id,username)、uk_tenant_mobile(tenant_id,mobile)。
-- mobile 为指针语义（未设置存 NULL），保证 uk_tenant_mobile 多空值不冲突；
-- post_ids 为 JSON 列，空值必须存 NULL（json 约束拒绝 ''）。
CREATE TABLE IF NOT EXISTS t_system_users (
    id          BIGINT       NOT NULL,
    creator     VARCHAR(64)  NULL,
    create_time DATETIME(3)  NULL,
    updater     VARCHAR(64)  NULL,
    update_time DATETIME(3)  NULL,
    deleted_at  DATETIME(3)  NULL,
    tenant_id   BIGINT       NOT NULL,
    username    VARCHAR(30)  NOT NULL,
    password    VARCHAR(100) NOT NULL,
    nickname    VARCHAR(30)  NOT NULL,
    remark      VARCHAR(500) NULL,
    dept_id     BIGINT       NULL,
    post_ids    JSON         NULL DEFAULT NULL,
    email       VARCHAR(50)  NULL,
    mobile      VARCHAR(20)  NULL,
    gender      TINYINT      NOT NULL DEFAULT 0,
    avatar      VARCHAR(512) NULL,
    status      TINYINT      NOT NULL DEFAULT 1,
    login_ip    VARCHAR(50)  NULL,
    login_date  DATETIME(3)  NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tenant_username (tenant_id, username),
    UNIQUE KEY uk_tenant_mobile (tenant_id, mobile),
    KEY idx_t_system_users_deleted_at (deleted_at),
    KEY idx_t_system_users_tenant_id (tenant_id),
    KEY idx_t_system_users_dept_id (dept_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
