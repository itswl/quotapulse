-- SQLite Localized messageÔLocalized messageVARCHAR/FLOAT/BOOLEAN/DATETIMEÔLocalized messageÔLocalized message
-- Localized message TEXT/REAL/INTEGERÔLocalized messageSQLite Localized messageÔLocalized message
-- Localized message DATETIME Localized message modernc.org/sqlite Localized message time.TimeÔLocalized message
-- Localized messageÔLocalized message CREATE ... IF NOT EXISTS Localized message

CREATE TABLE IF NOT EXISTS balance_history (
    id INTEGER NOT NULL,
    project_id VARCHAR(200) NOT NULL,
    project_name VARCHAR(200) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    balance FLOAT NOT NULL,
    threshold FLOAT,
    balance_type VARCHAR(20),
    need_alarm BOOLEAN,
    timestamp DATETIME,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS ix_balance_history_project_id ON balance_history (project_id);
CREATE INDEX IF NOT EXISTS ix_balance_history_provider ON balance_history (provider);
CREATE INDEX IF NOT EXISTS ix_balance_history_timestamp ON balance_history (timestamp);
CREATE INDEX IF NOT EXISTS idx_project_time ON balance_history (project_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_provider_time ON balance_history (provider, timestamp);

CREATE TABLE IF NOT EXISTS alert_history (
    id INTEGER NOT NULL,
    project_id VARCHAR(200) NOT NULL,
    project_name VARCHAR(200) NOT NULL,
    alert_type VARCHAR(50) NOT NULL,
    status VARCHAR(20),
    message TEXT,
    balance_value FLOAT,
    threshold_value FLOAT,
    timestamp DATETIME,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS ix_alert_history_project_id ON alert_history (project_id);
CREATE INDEX IF NOT EXISTS ix_alert_history_alert_type ON alert_history (alert_type);
CREATE INDEX IF NOT EXISTS ix_alert_history_timestamp ON alert_history (timestamp);
CREATE INDEX IF NOT EXISTS idx_project_type_time ON alert_history (project_id, alert_type, timestamp);

CREATE TABLE IF NOT EXISTS project_config (
    id INTEGER NOT NULL,
    name VARCHAR(200) NOT NULL,
    owner_project VARCHAR(200),
    provider VARCHAR(50) NOT NULL,
    api_key TEXT NOT NULL,
    threshold FLOAT,
    type VARCHAR(20),
    currency VARCHAR(10),
    monthly_budget FLOAT,
    enabled BOOLEAN,
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS ix_project_config_owner_project ON project_config (owner_project);

CREATE TABLE IF NOT EXISTS subscription_config (
    id INTEGER NOT NULL,
    name VARCHAR(200) NOT NULL,
    owner_project VARCHAR(200),
    cycle_type VARCHAR(20),
    renewal_day INTEGER,
    alert_days_before INTEGER,
    amount FLOAT,
    enabled BOOLEAN,
    last_renewed_date VARCHAR(20),
    snoozed_until VARCHAR(20),
    timezone VARCHAR(50),
    webhook_url TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS ix_subscription_config_owner_project ON subscription_config (owner_project);

CREATE TABLE IF NOT EXISTS email_config (
    id INTEGER NOT NULL,
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
    UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS email_alert_history (
    id INTEGER NOT NULL,
    mailbox VARCHAR(200) NOT NULL,
    sender VARCHAR(200) NOT NULL,
    subject VARCHAR(500) NOT NULL,
    date VARCHAR(100) NOT NULL,
    service_name VARCHAR(200),
    amount FLOAT,
    matched_keywords TEXT,
    alert_sent BOOLEAN,
    timestamp DATETIME,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS ix_email_alert_history_mailbox ON email_alert_history (mailbox);
CREATE INDEX IF NOT EXISTS ix_email_alert_history_timestamp ON email_alert_history (timestamp);
CREATE INDEX IF NOT EXISTS idx_email_mailbox_time ON email_alert_history (mailbox, timestamp);

CREATE TABLE IF NOT EXISTS email_suppressions (
    id INTEGER NOT NULL,
    mailbox VARCHAR(200) NOT NULL,
    sender VARCHAR(200) NOT NULL,
    created_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (mailbox, sender)
);

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id INTEGER NOT NULL,
    endpoint VARCHAR(500) NOT NULL,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (endpoint)
);

CREATE TABLE IF NOT EXISTS app_settings (
    setting_key VARCHAR(200) NOT NULL PRIMARY KEY,
    setting_value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS custom_provider_config (
    id INTEGER NOT NULL,
    name VARCHAR(50) NOT NULL,
    display_name VARCHAR(100),
    url TEXT NOT NULL,
    auth_mode VARCHAR(10) NOT NULL,
    auth_param VARCHAR(50),
    default_type VARCHAR(20) NOT NULL,
    currency VARCHAR(10),
    balance_path VARCHAR(200) NOT NULL,
    balance_scale FLOAT,
    success_path VARCHAR(200),
    success_values VARCHAR(200),
    error_path VARCHAR(200),
    subtract_path VARCHAR(200),
    extract_mode VARCHAR(20),
    window_types VARCHAR(100),
    window_reset_hours FLOAT,
    extra_params VARCHAR(400),
    sign_region VARCHAR(50),
    sign_service VARCHAR(50),
    created_at DATETIME,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS usage_events (
    id INTEGER NOT NULL,
    source VARCHAR(50) NOT NULL,
    project_name VARCHAR(200) NOT NULL,
    entry_date VARCHAR(10) NOT NULL,
    model VARCHAR(100) NOT NULL,
    cost DOUBLE NOT NULL,
    currency VARCHAR(10) NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    requests INTEGER NOT NULL,
    updated_at DATETIME,
    PRIMARY KEY (id),
    UNIQUE (source, project_name, entry_date, model)
);
