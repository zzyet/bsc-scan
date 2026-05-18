## ADDED Requirements

### Requirement: 监控合约注册
在数据库中维护需要监控的合约地址列表，支持直接操作数据库或后续管理后台添加。

#### Scenario: 添加监控合约
- **WHEN** 在 `monitored_contracts` 表插入一条记录
- **THEN** 记录包含合约地址（主键）、名称（可选）、添加时间、是否启用

#### Scenario: 停用/启用监控
- **WHEN** 修改合约的 `active` 字段
- **THEN** `active = false` 时该合约不参与扫描匹配；`active = true` 时恢复监控

#### Scenario: 数据来源
- **WHEN** 初期没有管理后台
- **THEN** 可直接在数据库中 INSERT 合约地址开始监控

### Requirement: 交易匹配
在 block-scanner 处理交易时，检测交易是否涉及已监控的合约。

#### Scenario: 匹配规则
- **WHEN** block-scanner 解析出一笔交易
- **THEN** 将交易的 `to` 地址和 `from` 地址与 `monitored_contracts` 表中 `active = true` 的合约地址比对，任一匹配即视为命中

#### Scenario: 未命中
- **WHEN** 交易的 from 和 to 均不在监控列表中
- **THEN** 跳过，不记录

### Requirement: 交易解析与记录
命中监控合约的交易，解析详细信息并持久化。

#### Scenario: 记录核心交易信息
- **WHEN** 一笔交易命中监控合约
- **THEN** 记录到 `contract_transactions` 表：

| 字段 | 来源 |
|------|------|
| tx_hash | transaction.hash |
| contract_address | 命中的合约地址 |
| block_number | block.number |
| block_timestamp | block.timestamp |
| from_addr | transaction.from |
| to_addr | transaction.to |
| value | transaction.value（wei） |
| gas_used | receipt.gasUsed |
| gas_price | transaction.gasPrice |
| status | receipt.status（1=成功 0=失败） |
| input_data | transaction.input（前 10 字节为方法选择器） |

#### Scenario: 解析 input data 方法签名
- **WHEN** 交易的 `input` 字段不为空
- **THEN** 提取前 4 字节（方法选择器）记录到 `method_selector` 字段，可选：通过 ABI 反查方法名

### Requirement: 事件日志关联
记录该交易产生的所有事件日志。

#### Scenario: 提取事件日志
- **WHEN** 交易收据中包含 `logs` 数组
- **THEN** 遍历每条 log，记录到 `contract_events` 表：

| 字段 | 来源 |
|------|------|
| tx_hash | 关联交易哈希 |
| log_index | log.logIndex |
| contract_address | log.address |
| topic_0 | log.topics[0]（事件签名哈希） |
| topic_1 | log.topics[1]（首个索引参数） |
| topic_2 | log.topics[2] |
| topic_3 | log.topics[3] |
| data | log.data（非索引参数 ABI 编码） |
| block_number | log.blockNumber |

### Requirement: 与 Block Scanner 的集成
合约监控作为 block-scanner 处理流程的一部分，在处理阶段触发。

#### Scenario: 嵌入处理流程
- **WHEN** block-scanner 处理阶段逐块解析交易
- **THEN** 每插入一笔交易到 `transactions` 表后，立即调用 Contract Monitor 检查该交易的 from/to 是否命中监控列表；命中则执行解析和记录

#### Scenario: 批处理优化
- **WHEN** 一个区块包含多笔交易
- **THEN** 先收集该区块所有命中的合约交易，批量 INSERT 到 `contract_transactions` 和 `contract_events` 表，减少数据库 round-trip
