-- t_user_oauth 三方账号绑定表（C 端 OAuth2 登录）。
--
-- 用途：支持「用 GitHub / Google / 微信登录」，把三方账号与本地 t_user 绑定。
-- 绑定是**可选的登录方式**而非账号主体：用户仍可用本地用户名口令登录。
--
-- 唯一索引（两个缺一不可）：
--   uk_provider_provider_id (provider, provider_user_id)
--     防「一个三方账号绑多个本地用户」—— 否则攻击者可用自己的三方账号
--     反复绑定他人本地账号（账号劫持）。
--   uk_user_provider (user_id, provider)
--     防「同一用户重复绑定同一提供方」，导致登录时不知该用哪条记录。
--
-- ⚠️ provider_user_id **必须**用三方的稳定标识（github numeric id /
--   google sub / wechat unionid，微信缺失时退 openid），
--   不可用 username / email / nickname——前者可改、后者可换、微信昵称可重复，
--   用它们当键会引发绑定错乱，且可被他人冒用。
--
-- ⚠️ access_token / refresh_token 是**高敏感凭据**：用于后续调用三方 API。
--   生产必须加密存储（应用层 AES-GCM，主密钥进密钥管理），禁止明文入库。
--   骨架期当前为明文，上线前必须改（见 model/oauth.go 的警告注释）。
CREATE TABLE IF NOT EXISTS t_user_oauth (
    id                BIGINT       NOT NULL,
    creator           VARCHAR(64)  NULL,
    create_time       DATETIME(3)  NULL,
    updater           VARCHAR(64)  NULL,
    update_time       DATETIME(3)  NULL,
    deleted_at        DATETIME(3)  NULL,
    user_id           BIGINT       NOT NULL,
    provider          VARCHAR(16)  NOT NULL,
    provider_user_id  VARCHAR(128) NOT NULL,
    nickname          VARCHAR(64)  NULL,
    avatar_url        VARCHAR(512)  NULL,
    access_token      VARCHAR(512)  NULL COMMENT '高敏感，上线前须加密',
    refresh_token     VARCHAR(512)  NULL COMMENT '高敏感，上线前须加密',
    expires_at        BIGINT       NULL COMMENT '三方 token 过期时间（Unix 秒）',
    PRIMARY KEY (id),
    UNIQUE KEY uk_provider_provider_id (provider, provider_user_id),
    UNIQUE KEY uk_user_provider (user_id, provider),
    KEY idx_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;