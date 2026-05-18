## ADDED Requirements

### Requirement: 整体架构
管理后台作为 Go 服务的内嵌 Web 应用，前后端分离，提供 RESTful API + SPA 前端。

#### Scenario: 访问入口
- **WHEN** 用户访问配置的 HTTP 端口（默认 8080）
- **THEN** 显示管理后台首页

### Requirement: Endpoint 管理
管理所有 BSC RPC 端点的配置。

#### Scenario: 列出 Endpoint
- **WHEN** 用户进入 Endpoint 管理页面
- **THEN** 表格展示所有 endpoint：URL、状态（healthy/unhealthy/circuit_open/stopped）、每分钟限流、今日请求数/日限额、连续失败/最大连续失败、累计失败/最大累计失败、操作按钮

#### Scenario: 添加 Endpoint
- **WHEN** 用户点击"添加"并填写表单
- **THEN** 表单字段：URL（必填）、rate_limit_per_minute、daily_limit、max_consecutive_failures、max_total_failures、backoff_initial、backoff_max、daily_reset_hour；提交后 INSERT 到 `endpoints` 表，Endpoint Manager 下次同步时自动加载

#### Scenario: 修改 Endpoint
- **WHEN** 用户点击某个 endpoint 的"编辑"
- **THEN** 弹出表单预填当前值，修改后 UPDATE `endpoints` 表，Endpoint Manager 下次同步时生效

#### Scenario: 删除 Endpoint
- **WHEN** 用户点击"删除"并确认
- **THEN** DELETE 该记录，Endpoint Manager 下次同步时移除对应 goroutine

#### Scenario: 停止/启用 Endpoint
- **WHEN** 用户点击"停止"或"启用"
- **THEN** 切换 `is_stopped` 字段，Endpoint Manager 下次同步时停止或恢复该 endpoint 的 goroutine

### Requirement: Block 管理
查看已扫描的区块列表。

#### Scenario: 列出区块
- **WHEN** 用户进入 Block 管理页面
- **THEN** 表格展示区块列表，字段：区块号、哈希、时间戳、交易数、状态（unprocessed/processing/processed）、处理时间，支持按状态筛选和按区块号排序

### Requirement: Contract 管理
管理需要监控的合约地址。

#### Scenario: 列出合约
- **WHEN** 用户进入 Contract 管理页面
- **THEN** 表格展示所有监控合约：地址、名称、是否启用、添加时间、最近交易时间、操作按钮

#### Scenario: 添加合约
- **WHEN** 用户点击"添加"并填写合约地址和名称
- **THEN** INSERT 到 `monitored_contracts` 表，Contract Monitor 立即开始监控该合约

#### Scenario: 修改合约
- **WHEN** 用户编辑合约名称或切换启用状态
- **THEN** UPDATE 对应记录，实时生效

#### Scenario: 删除合约
- **WHEN** 用户点击"删除"并确认
- **THEN** DELETE 该记录，停止监控该合约（已记录的历史数据保留）

### Requirement: Contract 交易信息
查看监控合约相关的交易和事件日志。

#### Scenario: 查看合约交易列表
- **WHEN** 用户进入 Contract 交易页面或点击某个合约的交易记录
- **THEN** 表格展示交易列表：交易哈希、区块号、时间、from、to、value、方法选择器、状态（成功/失败），支持按合约地址筛选

#### Scenario: 查看交易详情
- **WHEN** 用户点击某笔交易哈希
- **THEN** 展示完整交易信息：基本信息 + input data 详情 + 所有关联的 event logs（topic 和 data 解析）

#### Scenario: 查看事件日志
- **WHEN** 用户在交易详情页查看 event logs
- **THEN** 列表展示每条 log：log_index、contract_address、topic_0~3、data，附带 BSCScan 外链
