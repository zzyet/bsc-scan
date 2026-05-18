-- 核心引擎表
CREATE TABLE IF NOT EXISTS blocks (
    block_number BIGINT PRIMARY KEY,
    hash VARCHAR(66) NOT NULL,
    parent_hash VARCHAR(66),
    timestamp BIGINT NOT NULL,
    miner VARCHAR(42),
    gas_used BIGINT,
    gas_limit BIGINT,
    tx_count INT DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'unprocessed',
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_blocks_status ON blocks(status, block_number);

CREATE TABLE IF NOT EXISTS transactions (
    tx_hash VARCHAR(66) PRIMARY KEY,
    block_number BIGINT NOT NULL REFERENCES blocks(block_number),
    from_addr VARCHAR(42),
    to_addr VARCHAR(42),
    value NUMERIC(78),
    gas BIGINT,
    gas_price NUMERIC(78),
    input_data TEXT,
    status SMALLINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_txs_block ON transactions(block_number);
CREATE INDEX IF NOT EXISTS idx_txs_from ON transactions(from_addr);
CREATE INDEX IF NOT EXISTS idx_txs_to ON transactions(to_addr);

CREATE TABLE IF NOT EXISTS event_logs (
    id BIGSERIAL PRIMARY KEY,
    tx_hash VARCHAR(66) NOT NULL REFERENCES transactions(tx_hash),
    block_number BIGINT NOT NULL,
    log_index INT NOT NULL,
    address VARCHAR(42) NOT NULL,
    topic_0 VARCHAR(66),
    topic_1 VARCHAR(66),
    topic_2 VARCHAR(66),
    topic_3 VARCHAR(66),
    data TEXT,
    UNIQUE(tx_hash, log_index)
);
CREATE INDEX IF NOT EXISTS idx_logs_block ON event_logs(block_number);
CREATE INDEX IF NOT EXISTS idx_logs_address ON event_logs(address);

-- 管理配置表
CREATE TABLE IF NOT EXISTS endpoints (
    id BIGSERIAL PRIMARY KEY,
    url VARCHAR(512) NOT NULL,
    rate_limit_per_minute INT NOT NULL DEFAULT 60,
    daily_limit INT NOT NULL DEFAULT 0,
    max_consecutive_failures INT NOT NULL DEFAULT 5,
    max_total_failures INT NOT NULL DEFAULT 0,
    backoff_initial INT NOT NULL DEFAULT 60,
    backoff_max INT NOT NULL DEFAULT 600,
    daily_reset_hour INT NOT NULL DEFAULT 0,
    last_reset_time TIMESTAMPTZ,
    is_stopped BOOLEAN NOT NULL DEFAULT FALSE,
    daily_used INT NOT NULL DEFAULT 0,
    consecutive_failures INT NOT NULL DEFAULT 0,
    total_failures INT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS monitored_contracts (
    address VARCHAR(42) PRIMARY KEY,
    name VARCHAR(255),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ
);

-- 监控产出表
CREATE TABLE IF NOT EXISTS contract_transactions (
    tx_hash VARCHAR(66) PRIMARY KEY,
    contract_address VARCHAR(42) NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp BIGINT,
    from_addr VARCHAR(42),
    to_addr VARCHAR(42),
    value NUMERIC(78),
    gas_used BIGINT,
    gas_price NUMERIC(78),
    status SMALLINT,
    method_selector VARCHAR(10),
    input_data TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ct_contract ON contract_transactions(contract_address, block_number DESC);

CREATE TABLE IF NOT EXISTS contract_events (
    id BIGSERIAL PRIMARY KEY,
    tx_hash VARCHAR(66) NOT NULL REFERENCES contract_transactions(tx_hash),
    log_index INT NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    topic_0 VARCHAR(66),
    topic_1 VARCHAR(66),
    topic_2 VARCHAR(66),
    topic_3 VARCHAR(66),
    data TEXT,
    block_number BIGINT NOT NULL,
    UNIQUE(tx_hash, log_index)
);
