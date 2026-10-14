-- t_system_domain_event 领域事件表（DDD/CQRS 重构新增）。
--
-- 用途：承载聚合状态变更后「需要投影到外部读模型」的事实（当前投影目标为
-- casbin 策略）。事件与业务行在同一事务内写入，保证状态变更与待同步事实原子落库。
--
-- 投影语义：
--   - projected_at IS NULL 表示待投影；投影成功后写入时间戳；
--   - 投影失败不阻塞主流程，仅累加 attempts 并记录 last_error，由重试任务补偿。
--
-- 无种子数据（纯运行时表），故不放 sql/datas。
CREATE TABLE IF NOT EXISTS t_system_domain_event (
    id             BIGINT       NOT NULL,
    creator        VARCHAR(64)  NULL,
    create_time    DATETIME(3)  NULL,
    updater        VARCHAR(64)  NULL,
    update_time    DATETIME(3)  NULL,
    deleted_at     DATETIME(3)  NULL,
    event_name     VARCHAR(64)  NOT NULL,
    aggregate_type VARCHAR(32)  NOT NULL,
    aggregate_id   BIGINT       NOT NULL,
    tenant_id      BIGINT       NOT NULL,
    payload        JSON         NULL,
    occurred_at    DATETIME(3)  NOT NULL,
    projected_at   DATETIME(3)  NULL,
    attempts       INT          NOT NULL DEFAULT 0,
    last_error     VARCHAR(500) NULL,
    PRIMARY KEY (id),
    KEY idx_t_system_domain_event_event_name (event_name),
    KEY idx_t_system_domain_event_aggregate (aggregate_type, aggregate_id),
    KEY idx_t_system_domain_event_tenant_id (tenant_id),
    KEY idx_t_system_domain_event_projected_at (projected_at),
    KEY idx_t_system_domain_event_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
