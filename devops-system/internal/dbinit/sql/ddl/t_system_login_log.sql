-- t_system_login_log 登录审计表（租户表）：每次登录尝试（成功与失败）各记一行。
-- geo_* 来自 geo-ip 插件注入的 X-Higress-Geo-* 头；code 业务码与 auth_login 指标对齐。
CREATE TABLE IF NOT EXISTS t_system_login_log (
    id          BIGINT       NOT NULL,
    creator     VARCHAR(64)  NULL,
    create_time DATETIME(3)  NULL,
    updater     VARCHAR(64)  NULL,
    update_time DATETIME(3)  NULL,
    deleted_at  DATETIME(3)  NULL,
    tenant_id   BIGINT       NOT NULL,
    username    VARCHAR(30)  NOT NULL,
    user_id     BIGINT       NOT NULL DEFAULT 0,
    ip          VARCHAR(50)  NOT NULL,
    user_agent  VARCHAR(512) NULL,
    geo_country  VARCHAR(64) NULL,
    geo_province VARCHAR(64) NULL,
    geo_city     VARCHAR(64) NULL,
    geo_isp      VARCHAR(64) NULL,
    result      TINYINT      NOT NULL DEFAULT 0,
    code        BIGINT       NOT NULL DEFAULT 200,
    PRIMARY KEY (id),
    KEY idx_t_system_login_log_deleted_at (deleted_at),
    KEY idx_t_system_login_log_tenant_id (tenant_id),
    KEY idx_t_system_login_log_username (username),
    KEY idx_t_system_login_log_user_id (user_id),
    KEY idx_t_system_login_log_ip (ip)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
