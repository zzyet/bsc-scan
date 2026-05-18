## ADDED Requirements

### Requirement: 两阶段扫描流程
扫描分为**拉取**和**处理**两个独立阶段，区块有状态标记追踪进度。

```
拉取阶段                          处理阶段
───────                         ───────
DB 查最新已处理高度              SELECT status='unprocessed'
     ↓                               ↓
链上查最新高度                  逐块标记 'processing'
     ↓                               ↓
拉取新区块 → INSERT             解析交易 → INSERT txs
     ↓                               ↓
状态标记 'unprocessed'          解析收据 → INSERT logs
                                     ↓
                                标记 'processed'
```

### Requirement: 拉取阶段 — 获取新区块
从数据库获取最新已知高度，从链上获取最新高度，拉取缺失区块并入库。

#### Scenario: 计算拉取范围
- **WHEN** 拉取阶段启动
- **THEN** 查询 `blocks` 表获取最大 `block_number`（含 unprocessed 和 processed，避免重复拉取）作为 `last_db_height`；调用 `eth_blockNumber` 获取 `chain_latest_height`；拉取范围 = `(last_db_height + 1) ~ chain_latest_height`

#### Scenario: 首次启动
- **WHEN** `blocks` 表为空
- **THEN** `last_db_height` = 配置的 `start_block - 1`，从 `start_block` 开始拉取

#### Scenario: 批量拉取区块
- **WHEN** 确定拉取范围后
- **THEN** 通过 `eth_getBlockByNumber(blockNum, true)` 批量拉取（true 表示返回完整交易对象），每个区块 INSERT 到 `blocks` 表，`status = 'unprocessed'`

#### Scenario: 拉取完成
- **WHEN** 所有缺失区块拉取完毕
- **THEN** 拉取阶段结束，等待下一轮调度（默认每 3 秒检查一次链上新高度）

### Requirement: 区块状态机
每个区块在数据库中维护处理状态。

#### Scenario: 状态流转
- **WHEN** 区块被拉取入库
- **THEN** `status = 'unprocessed'`
- **WHEN** 处理阶段开始处理某区块
- **THEN** `status = 'processing'`
- **WHEN** 区块内所有交易和日志解析完成
- **THEN** `status = 'processed'`

### Requirement: 处理阶段 — 解析区块
查询所有 `unprocessed` 状态的区块，按区块号升序逐块处理。

#### Scenario: 获取待处理区块
- **WHEN** 处理阶段启动
- **THEN** `SELECT * FROM blocks WHERE status = 'unprocessed' ORDER BY block_number ASC LIMIT batch_size`（默认 batch_size = 50）

#### Scenario: 逐块处理
- **WHEN** 获取到待处理区块列表
- **THEN** 遍历每个区块：
  1. 更新 `status = 'processing'`
  2. 遍历区块内的 `transactions` 数组，逐条 INSERT 到 `transactions` 表
  3. 对每笔交易调用 `eth_getTransactionReceipt` 获取收据
  4. 从收据中提取 `logs` 数组，逐条 INSERT 到 `event_logs` 表
  5. 更新 `status = 'processed'`，记录 `processed_at` 时间戳

#### Scenario: 空区块
- **WHEN** 区块 `transactions` 数组为空
- **THEN** 直接将 `status` 从 `unprocessed` 更新为 `processed`，不插入交易和日志

### Requirement: 处理进度持久化
记录当前处理进度，支持断点恢复和监控。

#### Scenario: 进度查询
- **WHEN** 查询扫描进度
- **THEN** 返回：`unprocessed` 区块数、`processing` 区块数、`processed` 区块数、最新已处理高度

#### Scenario: processing 状态恢复
- **WHEN** 系统重启，存在 `status = 'processing'` 的区块
- **THEN** 将这些区块批量重置为 `unprocessed`，重新处理（防止上次异常中断导致数据不完整）

### Requirement: 并发处理
使用 worker pool 并发处理多个区块，通过 Endpoint Manager 获取令牌。

#### Scenario: Worker 获取令牌
- **WHEN** worker 准备处理一个区块
- **THEN** 从 Endpoint Manager 的租约池 `Acquire(ctx)` 获取令牌，处理完成后 `ReportSuccess()` 或 `ReportFailure()`

#### Scenario: 并发度控制
- **WHEN** 多个区块同时等待处理
- **THEN** worker pool 大小可配置（默认 5），每个 worker 独立获取租约，互不影响
