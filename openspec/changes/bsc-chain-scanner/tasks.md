## 1. 项目脚手架

- [ ] 1.1 初始化 Go module，创建目录结构
  - `go mod init bsc-scan`
  - 按 design.md 创建 `cmd/` `internal/` `admin/` `migrations/` 目录
  - `go get github.com/ethereum/go-ethereum`
  - `go get github.com/smartcontractkit/chainlink-common/pkg/services`

- [ ] 1.2 配置 go-zero & simple-admin-core 环境
  - `go install github.com/zeromicro/go-zero/tools/goctl@latest`
  - clone simple-admin-core 作为 admin 层参考
  - 引入 Ent ORM：`go get entgo.io/ent`

- [ ] 1.3 创建 config.yaml 并实现配置加载
  ```yaml
  # config.yaml
  database:
    dsn: "postgres://user:pass@localhost:5432/bsc_scan?sslmode=disable"
    max_connections: 20
  scanner:
    start_block: 48000000
    worker_count: 5
    batch_size: 50
    poll_interval: 3s
  sync_interval: 5m
  ```

- [ ] 1.4 编写 Orchestrator，集成 chainlink-common/services 生命周期
  ```go
  // cmd/bsc-scan/main.go
  type Orchestrator struct {
      endpointMgr *endpoint.Manager
      scanner     *scanner.Scanner
      monitor     *monitor.Monitor
  }
  func (o *Orchestrator) Start(ctx context.Context) error { ... }
  func (o *Orchestrator) Close() error { ... }
  ```

## 2. 数据库

- [ ] 2.1 编写 PostgreSQL migration 文件
  - 创建 `migrations/000001_init.up.sql`，按 design.md 表结构定义 7 张表
  - 创建 `migrations/000001_init.down.sql`
  - 使用 golang-migrate：`go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`

- [ ] 2.2 实现 store 包——数据库连接池
  ```go
  // internal/store/db.go
  func NewDB(dsn string, maxConns int) (*pgxpool.Pool, error) {
      config, _ := pgxpool.ParseConfig(dsn)
      config.MaxConns = int32(maxConns)
      return pgxpool.NewWithConfig(ctx, config)
  }
  ```

- [ ] 2.3 实现批量写入器（batch INSERT）
  - 支持 blocks、transactions、event_logs 批量 COPY 或多行 INSERT
  - 事务包裹：一个区块的所有数据原子写入

- [ ] 2.4 执行 migration，验证表结构
  ```bash
  migrate -database "$DSN" -path migrations up
  psql "$DSN" -c "\dt"
  ```

## 3. Endpoint Manager

- [ ] 3.1 实现 Endpoint 配置结构体 + Builder Pattern
  ```go
  // internal/endpoint/endpoint.go
  type Config struct {
      URL                  string
      RateLimitPerMinute   int
      DailyLimit           int
      MaxConsecutiveFailures int
      MaxTotalFailures     int
      BackoffInitial       time.Duration
      BackoffMax           time.Duration
      DailyResetHour       int
  }
  type Builder struct { ... }
  func (b *Builder) WithURL(url string) *Builder { ... }
  func (b *Builder) Build() *Endpoint { ... }
  ```

- [ ] 3.2 实现 Token Bucket——均匀生成令牌
  ```go
  // internal/endpoint/token_bucket.go
  // 根据 rate_limit_per_minute 计算间隔，ticker 定时生产令牌
  // 达到 daily_limit 后停止生产
  type TokenBucket struct {
      tokens chan struct{}
      // metrics
  }
  ```

- [ ] 3.3 实现租约机制
  ```go
  type Lease struct {
      endpoint *Endpoint
      release  func(success bool)
  }
  func (ep *Endpoint) Acquire(ctx context.Context) (*Lease, error)
  ```

- [ ] 3.4 实现 Circuit Breaker
  ```go
  // internal/endpoint/circuit_breaker.go
  // 状态机: healthy → circuit_open → half_open → healthy/dead
  // 指数退避: backoff_initial * 2^n, max backoff_max
  ```

- [ ] 3.5 实现 Endpoint Manager Service
  ```go
  // internal/endpoint/manager.go
  type Manager struct {
      endpoints map[string]*Endpoint  // goroutine per endpoint
      db        *pgxpool.Pool
  }
  func (m *Manager) Start(ctx context.Context) error   // services.Service
  func (m *Manager) syncFromDB(ctx context.Context)     // 5min interval
  func (m *Manager) saveStates(ctx context.Context)     // 5min interval
  ```

- [ ] 3.6 编写 Endpoint Manager 单元测试
  - 测试令牌生成速率
  - 测试熔断→恢复流程
  - 测试永久停止

## 4. Block Scanner

- [ ] 4.1 实现拉取阶段（Fetcher）
  ```go
  // internal/scanner/fetcher.go
  // 1. SELECT MAX(block_number) FROM blocks → lastDB
  // 2. eth_blockNumber → chainLatest
  // 3. 循环拉取 lastDB+1 到 chainLatest
  // 4. eth_getBlockByNumber(num, true) → INSERT blocks status='unprocessed'
  ```

- [ ] 4.2 实现处理阶段（Processor）
  ```go
  // internal/scanner/processor.go
  // 1. SELECT * FROM blocks WHERE status='unprocessed' ORDER BY number LIMIT batch_size
  // 2. 逐块 UPDATE status='processing'
  // 3. 遍历 transactions → INSERT transactions
  // 4. eth_getTransactionReceipt → INSERT event_logs
  // 5. UPDATE status='processed', processed_at=NOW()
  ```

- [ ] 4.3 实现 Worker Pool
  ```go
  // internal/scanner/worker_pool.go
  // 通过 Endpoint Manager 获取租约，控制并发
  type WorkerPool struct {
      workers   int
      leasePool *endpoint.Manager
  }
  ```

- [ ] 4.4 实现 Scanner Service
  ```go
  // internal/scanner/scanner.go
  func (s *Scanner) Start(ctx context.Context) error {
      // 启动 fetcher goroutine (3s interval)
      // 启动 processor goroutine (continuous)
  }
  ```

- [ ] 4.5 异常恢复——处理 processing 状态区块
  ```go
  // 启动时: UPDATE blocks SET status='unprocessed' WHERE status='processing'
  ```

- [ ] 4.6 编写 Scanner 单元测试
  - Mock RPC 客户端测试拉取/处理逻辑
  - 测试断点续扫
  - 测试空区块处理

## 5. Contract Monitor

- [ ] 5.1 实现合约匹配逻辑
  ```go
  // internal/monitor/monitor.go
  // 从 monitored_contracts 加载 active=true 的地址集合
  // 每笔交易的 from/to 与集合比对
  func (m *Monitor) Match(tx *types.Transaction) bool
  ```

- [ ] 5.2 实现交易解析
  ```go
  // internal/monitor/parser.go
  // 提取: tx_hash, from, to, value, gas_used, gas_price, status, input_data
  // method_selector = input_data[:10]
  // INSERT INTO contract_transactions
  ```

- [ ] 5.3 实现事件日志提取
  ```go
  // 从 receipt.Logs 遍历，提取 topic_0~3, data
  // INSERT INTO contract_events (UNIQUE tx_hash + log_index)
  ```

- [ ] 5.4 集成到 Scanner 处理流程
  ```go
  // processor.go 中，每处理完一笔交易，调用 monitor.Match(tx)
  // 命中则调用 monitor.ParseAndSave(tx, receipt)
  ```

- [ ] 5.5 合约列表定时同步
  ```go
  // 每 5 分钟从 DB 重新加载 monitored_contracts
  ```

- [ ] 5.6 编写 Contract Monitor 单元测试
  - 测试合约地址匹配（from 命中 / to 命中 / 都不命中）
  - 测试交易解析
  - 测试事件去重（UNIQUE 约束）

## 6. Admin 后台 API（go-zero）

- [ ] 6.1 创建 go-zero API 脚手架
  ```bash
  cd admin
  goctl api new bsc-admin --style go_zero
  # 生成: admin/api/bsc-admin.api, admin/internal/handler/, admin/internal/logic/, admin/etc/
  ```

- [ ] 6.2 编写 .api spec —— Endpoint 管理
  ```api
  // admin/api/desc/endpoint.api
  type EndpointInfo {
      Id                    int64  `json:"id"`
      Url                   string `json:"url"`
      RateLimitPerMinute    int    `json:"rateLimitPerMinute"`
      DailyLimit            int    `json:"dailyLimit"`
      // ... full fields
  }
  @server(group: endpoint)
  service bsc-admin {
      @handler createEndpoint
      post /api/endpoints (CreateEndpointReq) returns (BaseResp)
      @handler listEndpoints
      get /api/endpoints returns (ListEndpointResp)
      // ... CRUD + stop
  }
  ```

- [ ] 6.3 编写 .api spec —— Block / Contract / Transaction
  - Block: `GET /api/blocks` 查询区块列表（支持 status 筛选、分页）
  - Contract: CRUD `/api/contracts`
  - Transaction: `GET /api/contracts/:address/transactions`，`GET /api/transactions/:hash`

- [ ] 6.4 goctl 生成代码
  ```bash
  goctl api go -api api/desc/bsc-admin.api -dir . --style go_zero
  go mod tidy && go build ./...
  ```

- [ ] 6.5 实现 Logic 层——Endpoint CRUD
  ```go
  // admin/internal/logic/endpoint/create_endpoint_logic.go
  func (l *CreateEndpointLogic) CreateEndpoint(req *types.CreateEndpointReq) (*types.BaseResp, error) {
      _, err := l.svcCtx.DB.Exec(ctx,
          `INSERT INTO endpoints (url, rate_limit_per_minute, ...) VALUES ($1, $2, ...)`,
          req.Url, req.RateLimitPerMinute,
      )
      if err != nil {
          return nil, errorx.NewCodeError(500, err.Error())
      }
      return &types.BaseResp{Code: 0, Msg: "ok"}, nil
  }
  ```

- [ ] 6.6 实现 Logic 层——Block / Contract / Transaction 查询
  - 分页查询 blocks，支持 status 筛选
  - Contract CRUD 直接操作 monitored_contracts 表
  - Transaction 查询关联 contract_transactions + contract_events

- [ ] 6.7 配置 PostgreSQL 连接 + ServiceContext
  ```go
  // admin/internal/config/config.go
  type Config struct {
      rest.RestConf
      PgSQL struct {
          DSN string `json:",default=postgres://..."`
      }
  }
  // admin/internal/svc/service_context.go
  type ServiceContext struct {
      Config config.Config
      DB     *pgxpool.Pool
  }
  ```

- [ ] 6.8 编写 admin 的 etc 配置文件
  ```yaml
  # admin/etc/bsc-admin.yaml
  Name: bsc-admin
  Host: 0.0.0.0
  Port: 8080
  PgSQL:
    DSN: "postgres://user:pass@localhost:5432/bsc_scan?sslmode=disable"
  ```

- [ ] 6.9 编译验证
  ```bash
  cd admin && go mod tidy && go build -o bsc-admin ./bscadmin.go
  ```

## 7. Admin 前端（simple-admin-vben5-ui）

- [ ] 7.1 clone simple-admin-vben5-ui 作为基础模板
  ```bash
  cd /data/bsc-scan
  git clone --depth 1 https://github.com/suyuan32/simple-admin-vben5-ui.git frontend
  cd frontend && npm install
  ```

- [ ] 7.2 配置 API 代理指向 go-zero 后台
  - 修改 `vite.config.ts` proxy 指向 `http://localhost:8080`

- [ ] 7.3 新增页面——Endpoint 管理
  - 列表页：表格展示所有 endpoint，状态标签，操作按钮
  - 表单页：新增/编辑 endpoint 参数

- [ ] 7.4 新增页面——Block 浏览
  - 表格展示区块列表，按状态标签筛选

- [ ] 7.5 新增页面——Contract 管理 + 交易浏览
  - Contract 列表 + 新增/编辑表单
  - 点击合约查看关联交易

- [ ] 7.6 验证完整流程：前端 → go-zero API → PostgreSQL
  ```bash
  # 启动 admin API
  cd admin && ./bsc-admin &
  # 启动前端 dev server
  cd frontend && npm run dev
  ```

## 8. 集成与部署

- [ ] 8.1 核心引擎所有 Service 集成到 Orchestrator
  - Endpoint Manager → Scanner → Monitor 依赖顺序启动
  - Graceful shutdown：Monitor → Scanner → Endpoint Manager

- [ ] 8.2 端到端测试（BSC 测试网）
  - 连接 BSC Testnet RPC
  - 添加一个 ERC20 合约地址到监控列表
  - 启动扫描，验证数据入库
  - 通过 admin API 查询验证

- [ ] 8.3 编写 Dockerfile
  ```dockerfile
  FROM golang:1.22-alpine AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -o bsc-scan ./cmd/bsc-scan

  FROM alpine:latest
  COPY --from=builder /app/bsc-scan /usr/local/bin/
  COPY --from=builder /app/migrations /migrations
  ENTRYPOINT ["bsc-scan"]
  ```

- [ ] 8.4 编写 README.md
  - 项目概述
  - 环境要求（Go 1.22+, PostgreSQL 15+, Node.js 20+）
  - 快速开始（初始化 DB → 启动核心引擎 → 启动 Admin）
  - 配置说明
  - API 文档链接
