-- t_user C 端用户表。
--
-- 与 devops-system 的 t_system_users 的关键差异：
--   - 无 tenant_id：C 端单租户，不做多租户隔离；
--   - 无 dept_id / post_ids / gender / status / login_ip / login_date：C 端无组织架构与后台审计字段；
--   - disabled 取代 status：C 端只有「正常/封禁」两态。
--
-- 唯一索引说明：
--   - uk_username：C 端无租户维度，用户名全局唯一；
--   - uk_email / uk_mobile：**必须让未填写存 NULL**（列定义无 NOT NULL）——
--     MySQL 唯一索引不约束多个 NULL，若用空串则第二个未绑定邮箱的用户即冲突。
--     应用层 model.normalizePtr 负责把空串归一化为 NULL。
CREATE TABLE IF NOT EXISTS t_user (
    id          BIGINT       NOT NULL,
    creator     VARCHAR(64)  NULL,
    create_time DATETIME(3)  NULL,
    updater     VARCHAR(64)  NULL,
    update_time DATETIME(3)  NULL,
    deleted_at  DATETIME(3)  NULL,
    username    VARCHAR(32)  NOT NULL,
    password    VARCHAR(100) NOT NULL COMMENT '口令哈希，勿存明文',
    nickname    VARCHAR(32)  NOT NULL,
    email       VARCHAR(100) NULL,
    mobile      VARCHAR(20)  NULL,
    avatar      VARCHAR(512) NULL,
    disabled    TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '0正常 1封禁',
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username),
    UNIQUE KEY uk_email (email),
    UNIQUE KEY uk_mobile (mobile),
    KEY idx_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;