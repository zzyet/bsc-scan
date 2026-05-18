## ADDED Requirements

### Requirement: Endpoint 配置模型
每个 Endpoint 包含完整的运行时参数，从数据库加载并支持定期同步。

#### Scenario: Endpoint 参数定义
- **WHEN** 定义一个 BSC RPC Endpoint
- **THEN** 包含以下字段：

| 字段 | 说明 | 默认值 |
|------|------|--------|
| URL | 节点 RPC 地址 | 必填 |
| rate_limit_per_minute | 每分钟请求上限，均匀生成令牌 | 60 |
| daily_limit | 每日请求上限（0=无限） | 0 |
| max_consecutive_failures | 连续失败次数上限，触发熔断（0=无限） | 5 |
| max_total_failures | 累计失败上限，永久停止节点（0=无限） | 0 |
| backoff_initial | 初始退避时间 | 1min |
| backoff_max | 最大退避时间 | 10min |
| daily_reset_hour | 每日请求计数重置的 UTC 小时 | 0 |
| last_reset_time | 上次重置时间 | auto |
| is_stopped | 是否停止该节点 | false |

### Requirement: 数据库同步
定时从数据库同步节点列表，运行时状态定时回写。

#### Scenario: 定时拉取节点列表
- **WHEN** 系统运行中
- **THEN** 每 5 分钟从 `endpoints` 表全量拉取节点配置，对比当前运行列表，新增/更新/移除对应 goroutine

#### Scenario: 定时保存运行时状态
- **WHEN** 系统运行中
- **THEN** 每 5 分钟将每个 endpoint 的当前状态（请求计数、失败计数、熔断状态、令牌可用数）写回 `endpoints` 表

### Requirement: 令牌桶生成
每个 endpoint 独立 goroutine，按 rate_limit_per_minute 均匀生成令牌。

#### Scenario: 均匀令牌生成
- **WHEN** 某 endpoint 配置 `rate_limit_per_minute = 10`
- **THEN** 该 endpoint 的 goroutine 每隔 6 秒生成 1 个令牌，投入共享令牌队列

#### Scenario: 日限额耗尽
- **WHEN** 某 endpoint 24 小时内请求数达到 `daily_limit`（且 daily_limit > 0）
- **THEN** 停止生成令牌，消费者获取令牌时返回错误 `ErrDailyQuotaExceeded`

### Requirement: 消费者租约
消费者从共享令牌队列获取令牌，请求完成后报告结果。

#### Scenario: 获取令牌
- **WHEN** 消费者调用 `Acquire(ctx)` 
- **THEN** 阻塞等待直到队列中有可用令牌，返回租约（含 endpoint 引用和释放回调）

#### Scenario: 报告成功
- **WHEN** 消费者调用 `lease.ReportSuccess()`
- **THEN** 该 endpoint 的请求计数 +1，连续失败计数归零

#### Scenario: 报告失败
- **WHEN** 消费者调用 `lease.ReportFailure()`
- **THEN** 该 endpoint 的连续失败计数 +1，累计失败计数 +1

### Requirement: 故障熔断
连续失败达到上限触发熔断，指数退避恢复。

#### Scenario: 熔断触发
- **WHEN** 某 endpoint 的连续失败次数达到 `max_consecutive_failures`（且 > 0）
- **THEN** endpoint 进入 `circuit_open` 状态，暂停令牌生成，开始退避计时器

#### Scenario: 指数退避恢复
- **WHEN** 退避计时器到期
- **THEN** endpoint 进入 `half_open` 状态，允许生成 1 个探测令牌；如果探测请求成功 → 恢复到 `healthy`，失败 → 重新熔断，退避时间翻倍（最大 `backoff_max`）

### Requirement: 永久停止
累计失败超上限后永久停止节点。

#### Scenario: 累计失败触发永久停止
- **WHEN** 某 endpoint 累计失败次数达到 `max_total_failures`（且 > 0）
- **THEN** endpoint 的 `is_stopped` 设为 true，goroutine 退出，状态同步到数据库，不再接受任何请求

### Requirement: Builder Pattern 初始化
使用链式调用构造 Endpoint 实例。

#### Scenario: Builder 构造
- **WHEN** 创建 Endpoint 实例
- **THEN** 使用 Builder Pattern：
```go
ep := endpoint.NewBuilder().
    WithURL("https://bsc-dataseed.binance.org").
    WithRateLimit(60).
    WithDailyLimit(100000).
    WithMaxConsecutiveFailures(5).
    WithMaxTotalFailures(100).
    WithBackoff(1*time.Minute, 10*time.Minute).
    WithDailyResetHour(0).
    Build()
```

### Requirement: chainlink-common/services 生命周期
每个 endpoint goroutine 通过 chainlink-common Service 接口管理。

#### Scenario: Service 生命周期
- **WHEN** EndpointManager 启动
- **THEN** 为每个 endpoint 创建实现 `services.Service` 接口的 goroutine，统一 Start/Close/HealthReport 管理
