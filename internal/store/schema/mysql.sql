-- MySQL Localized message utf8mb4ÔLocalized message latin1 Localized message utf8mb3ÔLocalized message
-- Localized message 1366ÔLocalized message JOIN/Localized message 1267ÔLocalized message utf8mb4_unicode_ciLocalized message
-- MySQL Localized message CREATE INDEX Localized message IF NOT EXISTSÔLocalized message CREATE TABLE IF NOT EXISTS Localized messageÔLocalized message
-- Localized message
-- timestamp Localized messageÔLocalized message

CREATE TABLE IF NOT EXISTS balance_history (
    id BIGINT NOT NULL AUTO_INCREMENT,
    project_id VARCHAR(200) NOT NULL,
    project_name VARCHAR(200) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    balance DOUBLE NOT NULL,
    threshold DOUBLE,
    balance_type VARCHAR(20),
    need_alarm BOOLEAN,
    `timestamp` DATETIME,
    PRIMARY KEY (id),
    KEY ix_balance_history_project_id (project_id),
    KEY ix_balance_history_provider (provider),
    KEY ix_balance_history_timestamp (`timestamp`),
    KEY idx_project_time (project_id, `timestamp`),
    KEY idx_provider_time (provider, `timestamp`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS alert_history (
    id BIGINT NOT NULL AUTO_INCREMENT,
    project_id VARCHAR(200) NOT NULL,
    project_name VARCHAR(200) NOT NULL,
    alert_type VARCHAR(50) NOT NULL,
    status VARCHAR(20),
    message TEXT,
    balance_value DOUBLE,
    threshold_value DOUBLE,
    `timestamp` DATETIME,
    PRIMARY KEY (id),
    KEY ix_alert_history_project_id (project_id),
    KEY ix_alert_history_alert_type (alert_type),
    KEY ix_alert_history_timestamp (`timestamp`),
    KEY idx_project_type_time (project_id, alert_type, `timestamp`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS project_config (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(200) NOT NULL,
    owner_project VARCHAR(200),
    provider VARCHAR(50) NOT NULL,
    api_key TEXT NOT NULL,
    threshold DOUBLE,
    type VARCHAR(20),
    enabled BOOLEAN,
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE KEY uq_project_config_name (name),
    KEY ix_project_config_owner_project (owner_project)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS subscription_config (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(200) NOT NULL,
    owner_project VARCHAR(200),
    cycle_type VARCHAR(20),
    renewal_day INTEGER,
    alert_days_before INTEGER,
    amount DOUBLE,
    enabled BOOLEAN,
    last_renewed_date VARCHAR(20),
    snoozed_until VARCHAR(20),
    timezone VARCHAR(50),
    webhook_url TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE KEY uq_subscription_config_name (name),
    KEY ix_subscription_config_owner_project (owner_project)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS email_config (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(200) NOT NULL,
    host VARCHAR(200) NOT NULL,
    port INTEGER,
    username VARCHAR(200) NOT NULL,
    password TEXT NOT NULL,
    use_ssl BOOLEAN,
    enabled BOOLEAN,
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE KEY uq_email_config_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS email_alert_history (
    id BIGINT NOT NULL AUTO_INCREMENT,
    mailbox VARCHAR(200) NOT NULL,
    sender VARCHAR(200) NOT NULL,
    subject VARCHAR(500) NOT NULL,
    date VARCHAR(100) NOT NULL,
    service_name VARCHAR(200),
    amount DOUBLE,
    matched_keywords TEXT,
    alert_sent BOOLEAN,
    `timestamp` DATETIME,
    PRIMARY KEY (id),
    KEY ix_email_alert_history_mailbox (mailbox),
    KEY ix_email_alert_history_timestamp (`timestamp`),
    KEY idx_email_mailbox_time (mailbox, `timestamp`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS email_suppressions (
    id BIGINT NOT NULL AUTO_INCREMENT,
    mailbox VARCHAR(200) NOT NULL,
    sender VARCHAR(200) NOT NULL,
    created_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE KEY uq_email_suppressions_mailbox_sender (mailbox, sender)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

