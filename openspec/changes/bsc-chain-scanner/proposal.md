## Why

BSC 链上每天产生数百万笔交易和事件，目前缺乏一个轻量级、可自部署的实时扫描监控系统。现有方案要么依赖第三方 API（延迟高、成本不可控），要么是重型索引器（Erigon/The Graph）部署复杂。需要一个针对 BSC 优化的、模块化的链上数据扫描与监控系统，支持多节点管理、全量扫描、合约事件监控和大额交易告警。

## What Changes

- 新增 BSC 链全量区块扫描引擎，从指定高度逐块拉取并解析交易和事件日志
- 新增多 RPC 节点管理器，支持租约机制、速率限制、日限额、故障熔断与自动恢复
- 新增 ERC20 合约 Transfer 事件监控，支持多合约配置化监听
- 新增交易告警引擎，基于用户定义条件触发通知
- 新增 PostgreSQL 数据持久化层，存储区块、交易、日志及告警事件
- 新增 Web 管理后台，提供数据查询、合约管理、告警配置和可视化展示

## Capabilities

### New Capabilities
- `endpoint-manager`: 多 BSC RPC 端点生命周期管理，租约机制，速率限制，日限额，故障熔断与自动恢复
- `block-scanner`: 从指定高度逐块全量扫描 BSC 链上区块、交易及事件日志
- `contract-monitor`: 监控指定合约的交易交互，解析交易详情和事件日志
- `admin-dashboard`: Web 管理后台，数据查询、合约管理、可视化面板

### Modified Capabilities
<!-- 新项目，无已有 capability 需修改 -->

## Impact

- **语言 & 框架**: Go 1.22+, chainlink-common/services 统一服务生命周期
- **数据库**: PostgreSQL 15+
- **外部依赖**: BSC RPC 节点（可配置多个）、通知渠道（Telegram/Webhook）
- **部署**: 单二进制部署，Docker 可选
