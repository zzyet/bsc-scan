# BSC Scan — BSC 区块链扫描 + 合约监控

AI 驱动的 BSC 区块链数据采集系统，支持多 RPC 端点管理、区块自动扫描、合约交易监控和分析。

## 架构

```
┌──────────────────────────────────────────────────────┐
│                    Orchestrator                       │
│  ┌──────────────┐ ┌──────────┐ ┌─────────────────┐  │
│  │ Endpoint Mgr │ │ Scanner  │ │ Contract Monitor │  │
│  │ 令牌桶+熔断   │ │ 拉取→处理 │ │ 合约匹配+事件    │  │
│  └──────────────┘ └──────────┘ └─────────────────┘  │
│                        │                             │
│                   PostgreSQL                         │
└──────────────────────────────────────────────────────┘
                         │
              ┌──────────┴──────────┐
              │    Admin API :8080  │
              │    HTML Dashboard   │
              └─────────────────────┘
```

- **Endpoint Manager**: 多 RPC 节点管理，令牌桶限速，熔断器故障隔离，指数退避恢复
- **Block Scanner**: 两阶段扫描（拉取 → 处理），Worker Pool 并发，断点续扫
- **Contract Monitor**: 监控合约交易匹配，事件日志提取，Transfer 事件实时解析
- **Admin API**: RESTful API + 单页管理面板，支持 Endpoint/Block/Contract CRUD

## 环境要求

| 组件 | 版本 |
|------|------|
| Go | 1.24+ |
| PostgreSQL | 16+ |
| Docker | 可选 |
| Node.js | 20+ (前端开发) |

## 快速开始

### Docker 部署（推荐）

```bash
# 启动全部服务（PostgreSQL + 核心引擎 + Admin）
docker compose up -d

# 查看日志
docker compose logs -f bsc-scan

# 打开管理面板
open http://localhost:8080
```

### 本地开发

```bash
# 安装依赖
go mod download

# 启动 PostgreSQL
docker run -d --name bsc-pg -e POSTGRES_USER=bsc -e POSTGRES_PASSWORD=bsc123 \
  -e POSTGRES_DB=bsc_scan -p 5432:5432 postgres:16-alpine

# 执行 migration
psql postgres://bsc:bsc123@localhost:5432/bsc_scan?sslmode=disable \
  < migrations/000001_init.up.sql
psql postgres://bsc:bsc123@localhost:5432/bsc_scan?sslmode=disable \
  < migrations/000002_seed.sql

# 启动核心扫描引擎
go run ./cmd/bsc-scan

# 启动 Admin API（另一个终端）
go run ./admin
```

## 配置 (config.yaml)

```yaml
database:
  dsn: "postgres://bsc:bsc123@postgres:5432/bsc_scan?sslmode=disable"
  max_connections: 20
scanner:
  start_block: 0          # 0 = 从最新区块开始; >0 = 断点续扫
  worker_count: 5         # 并发处理 worker 数
  batch_size: 50          # 每批处理区块数
  poll_interval: 3s       # 拉取间隔
sync_interval: 5m         # Endpoint/Contract 同步间隔
```

## API 文档

Base URL: `http://localhost:8080`

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/stats` | 系统统计 |
| GET | `/api/endpoints` | Endpoint 列表 |
| POST | `/api/endpoints` | 新增 Endpoint |
| GET | `/api/endpoints/:id` | Endpoint 详情 |
| PUT | `/api/endpoints/:id` | 更新 Endpoint |
| DELETE | `/api/endpoints/:id` | 删除 Endpoint |
| POST | `/api/endpoints/:id/stop` | 停止 Endpoint |
| GET | `/api/blocks?status=&page=&limit=` | 区块列表 |
| GET | `/api/blocks/:number` | 区块详情 |
| GET | `/api/contracts` | 监控合约列表 |
| POST | `/api/contracts?address=` | 新增监控合约 |
| PUT | `/api/contracts/:address` | 更新合约 |
| DELETE | `/api/contracts/:address` | 删除合约 |
| GET | `/api/contracts/:address/transactions` | 合约交易记录 |
| GET | `/api/transactions/:hash` | 交易详情 + 事件日志 |

## 数据库表

| 表 | 说明 |
|----|------|
| blocks | 区块数据 |
| transactions | 交易数据 |
| event_logs | 事件日志 |
| endpoints | RPC 端点配置 |
| monitored_contracts | 监控合约列表 |
| contract_transactions | 合约交易 |
| contract_events | 合约事件 |

## 测试

```bash
go test ./... -count=1
```

## 项目结构

```
bsc-scan/
├── cmd/bsc-scan/       # 核心引擎入口
├── admin/              # Admin HTTP API + 前端
│   ├── main.go
│   ├── handler/        # API handlers
│   └── frontend/       # 管理面板 HTML
├── internal/
│   ├── config/         # 配置加载
│   ├── endpoint/       # Endpoint 管理器
│   ├── scanner/        # 区块扫描器
│   ├── monitor/        # 合约监控
│   ├── orchestrator/   # 服务编排
│   └── store/          # 数据库操作
├── migrations/         # SQL 迁移
├── openspec/           # 规格文档 (OpenSpec)
├── Dockerfile
├── docker-compose.yml
└── config.yaml
```

## License

MIT
