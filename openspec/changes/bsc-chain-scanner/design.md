## Context

BSC 兼容 EVM，平均出块时间 3 秒，每日约 28,800 个区块。需要系统跟上出块速度，同时管理多个 RPC 端点实现高可用。

项目分为两层：
- **核心引擎层**：链上数据扫描与监控，Go + chainlink-common/services 统一生命周期
- **管理后台层**：Web 管理界面，Go-Zero + simple-admin-core 架构

## Goals / Non-Goals

**Goals:**
- 实时全量扫描 BSC 链，不丢块，支持断点续扫
- 多 RPC 端点自动故障切换，单节点故障不影响扫描
- 命中监控合约的交易自动解析并记录
- PostgreSQL 存储所有链上数据和配置
- 管理后台支持 Endpoint / Block / Contract / 交易信息的 CRUD

**Non-Goals:**
- 不支持非 EVM 链
- 不做 DEX 价格或交易分析
- 不做实时 WebSocket 推送（首版仅 poll）
- 不做告警通知（transaction-alert 本次不做）

## Decisions

### 1. 分层架构

```
┌─────────────────────────────────────────┐
│           Admin Dashboard               │
│    go-zero + simple-admin-core          │
│    Ent ORM + Casbin RBAC                │
│    Vben5 UI (simple-admin-vben5-ui)     │
├─────────────────────────────────────────┤
│           Core Engine                   │
│    chainlink-common/services            │
│    ┌──────────┐ ┌──────────┐           │
│    │ Endpoint  │ │  Block   │           │
│    │ Manager   │ │ Scanner  │           │
│    └──────────┘ └──────────┘           │
│    ┌──────────┐                         │
│    │ Contract │                         │
│    │ Monitor  │                         │
│    └──────────┘                         │
├─────────────────────────────────────────┤
│           PostgreSQL                    │
│    blocks / transactions / event_logs   │
│    contract_transactions / contract_events│
│    endpoints / monitored_contracts      │
└─────────────────────────────────────────┘
```

### 2. 技术栈明细

| 层 | 技术 | 说明 |
|----|------|------|
| 核心引擎 | Go 1.22+ | 主语言 |
| 服务生命周期 | chainlink-common/services | Start/Close/HealthReport |
| RPC 客户端 | go-ethereum/ethclient | JSON-RPC 调用 |
| 后台框架 | go-zero v1.10+ | RESTful API + 中间件 |
| ORM | Ent (entgo.io) | 类型安全 ORM，simple-admin 标配 |
| 权限 | Casbin | RBAC，simple-admin 集成 |
| 前端 | simple-admin-vben5-ui | Vue3 + Vben Admin，开箱即用 |
| 数据库 | PostgreSQL 15+ | pgx/v5 驱动 |
| 迁移 | golang-migrate 或 Ent migrate | schema 版本管理 |

### 3. 项目结构

```
bsc-scan/
├── cmd/
│   └── bsc-scan/main.go          # 核心引擎入口
├── internal/
│   ├── endpoint/                  # Endpoint Manager
│   │   ├── manager.go             # Service 实现
│   │   ├── endpoint.go            # Endpoint 实例 + Builder
│   │   ├── token_bucket.go        # 令牌桶
│   │   └── circuit_breaker.go     # 熔断器
│   ├── scanner/                   # Block Scanner
│   │   ├── scanner.go             # Service 实现
│   │   ├── fetcher.go             # 拉取阶段
│   │   ├── processor.go           # 处理阶段
│   │   └── worker_pool.go         # 并发控制
│   ├── monitor/                   # Contract Monitor
│   │   ├── monitor.go             # 合约匹配 + 解析
│   │   └── parser.go              # input/log 解析
│   └── store/                     # 数据访问层
│       ├── db.go                  # pgx 连接池
│       └── queries/               # SQL 查询
├── admin/                         # 管理后台 (go-zero)
│   ├── api/                       # .api 定义文件
│   ├── internal/
│   │   ├── config/                # go-zero 配置
│   │   ├── handler/               # HTTP handler
│   │   ├── logic/                 # 业务逻辑
│   │   ├── svc/                   # ServiceContext
│   │   └── middleware/            # Casbin 中间件
│   └── ent/                       # Ent schema & generated
│       └── schema/                # 表定义
├── frontend/                      # Vben5 前端 (symbolic link or submodule)
├── migrations/                    # SQL 迁移文件
├── config.yaml                    # 核心引擎配置
└── go.mod
```

### 4. 数据库表（PostgreSQL）

**核心引擎表：**

```sql
-- 区块
blocks (
    block_number BIGINT PRIMARY KEY,
    hash VARCHAR(66) NOT NULL,
    parent_hash VARCHAR(66),
    timestamp BIGINT NOT NULL,
    miner VARCHAR(42),
    gas_used BIGINT,
    gas_limit BIGINT,
    tx_count INT,
    status VARCHAR(16) DEFAULT 'unprocessed',  -- unprocessed/processing/processed
    processed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 交易
transactions (
    tx_hash VARCHAR(66) PRIMARY KEY,
    block_number BIGINT REFERENCES blocks(block_number),
    from_addr VARCHAR(42),
    to_addr VARCHAR(42),
    value NUMERIC(78),
    gas BIGINT,
    gas_price NUMERIC(78),
    input_data TEXT,
    status SMALLINT,  -- 1=success 0=fail
    created_at TIMESTAMP DEFAULT NOW()
);

-- 事件日志
event_logs (
    id BIGSERIAL PRIMARY KEY,
    tx_hash VARCHAR(66) REFERENCES transactions(tx_hash),
    block_number BIGINT,
    log_index INT,
    address VARCHAR(42),
    topic_0 VARCHAR(66),
    topic_1 VARCHAR(66),
    topic_2 VARCHAR(66),
    topic_3 VARCHAR(66),
    data TEXT,
    UNIQUE(tx_hash, log_index)
);
```

**管理配置表：**

```sql
-- RPC 端点
endpoints (
    id BIGSERIAL PRIMARY KEY,
    url VARCHAR(255) NOT NULL,
    rate_limit_per_minute INT DEFAULT 60,
    daily_limit INT DEFAULT 0,
    max_consecutive_failures INT DEFAULT 5,
    max_total_failures INT DEFAULT 0,
    backoff_initial INT DEFAULT 60,
    backoff_max INT DEFAULT 600,
    daily_reset_hour INT DEFAULT 0,
    last_reset_time TIMESTAMP,
    is_stopped BOOLEAN DEFAULT FALSE,
    -- 运行时状态（定时回写）
    daily_used INT DEFAULT 0,
    consecutive_failures INT DEFAULT 0,
    total_failures INT DEFAULT 0,
    status VARCHAR(16) DEFAULT 'unknown',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP
);

-- 监控合约
monitored_contracts (
    address VARCHAR(42) PRIMARY KEY,
    name VARCHAR(255),
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP
);

-- 合约交易
contract_transactions (
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
    created_at TIMESTAMP DEFAULT NOW()
);

-- 合约事件
contract_events (
    id BIGSERIAL PRIMARY KEY,
    tx_hash VARCHAR(66) REFERENCES contract_transactions(tx_hash),
    log_index INT,
    contract_address VARCHAR(42),
    topic_0 VARCHAR(66),
    topic_1 VARCHAR(66),
    topic_2 VARCHAR(66),
    topic_3 VARCHAR(66),
    data TEXT,
    block_number BIGINT,
    UNIQUE(tx_hash, log_index)
);
```

### 5. 核心引擎与后台的关系

- **核心引擎** 直连 PostgreSQL，写入 blocks/transactions/event_logs/contract_transactions/contract_events
- **管理后台** 通过 go-zero API 提供 CRUD 接口，读写 endpoints/monitored_contracts，查询核心引擎写入的数据
- 两者共享同一个 PostgreSQL 数据库
- 核心引擎定时从 endpoints 表同步配置（5 分钟），后台修改 endpoint 后核心引擎下次同步生效

### 6. 后台 API 设计 (go-zero)

```
// Endpoint 管理
POST   /api/endpoints          → 添加
GET    /api/endpoints          → 列表
PUT    /api/endpoints/:id      → 修改
DELETE /api/endpoints/:id      → 删除
POST   /api/endpoints/:id/stop → 停止/启用

// Block 查询
GET    /api/blocks             → 列表（支持 status 筛选）

// Contract 管理
POST   /api/contracts          → 添加
GET    /api/contracts          → 列表
PUT    /api/contracts/:address → 修改
DELETE /api/contracts/:address → 删除

// Contract 交易
GET    /api/contracts/:address/transactions → 交易列表
GET    /api/transactions/:hash              → 交易详情 + event logs
```

### 7. Ent Schema 定义策略

使用 Ent 的 schema 定义来管理所有表的 ORM 映射，simple-admin-core 的代码生成工具可以自动生成 handler/logic 层代码。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| BSC RPC 限流 | 多节点池 + token bucket + 熔断自动切换 |
| 链重组数据不一致 | 处理阶段检查 parent_hash 连续性 |
| 全量扫描跟不上出块 | worker pool 并发 + 批量 INSERT |
| 核心引擎和后台共享 DB | 读写分离后续版本考虑，首版单库够用 |
| simple-admin-core 版本锁定 | 固定版本，不自动升级 |
