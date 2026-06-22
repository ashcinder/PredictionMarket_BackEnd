# MySQL 市场历史与 AI 决策持久化设计

## 目标与范围

将当前 AI 托管引擎的进程内市场历史迁移到 MySQL 8，使真实 YES/NO 占比历史在后端重启后仍然存在，并为后续前端折线图提供统一的只读历史 API。同时持久化规则 HOLD 与模型决策，形成可审计的交易记录。

本轮只修改 Go 后端、后端配置、数据库 migration、测试和部署说明，不修改 `agent/` 前端。托管启停状态、用户私钥和私钥密文继续保存在进程内；服务重启后用户需要重新开启托管。数据库中不保存钱包私钥。

## 技术选型

使用 Go 标准库 `database/sql`、`github.com/go-sql-driver/mysql` 和显式版本化 SQL migration。AI 托管引擎依赖仓储接口，不直接依赖 MySQL 驱动或 SQL 语句。

不使用 GORM 或 AutoMigrate，避免隐藏表结构和查询行为；不采用内存与 MySQL 双写，避免两份状态不一致。MySQL 是市场历史和决策审计的唯一持久化来源，内存只保存托管任务及解密所需的进程级密钥。

## 启动与生命周期

YAML 新增：

```yaml
mysql:
  dsn: "user:password@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"
  max_open_connections: 10
  max_idle_connections: 5
  connection_max_lifetime_seconds: 300
```

所有字段使用现有严格 YAML 解析。DSN 必填且必须能被 MySQL 驱动解析；三个连接池数值必须为正数，空闲连接数不得大于最大连接数。真实 DSN 只写入已被 Git 忽略的 `config.yaml`，公开示例使用占位符。日志和错误响应不得输出完整 DSN 或数据库密码。

后端启动顺序：

1. 加载并校验 YAML。
2. 创建 MySQL 连接池并应用连接池参数。
3. 使用有超时的上下文执行 `PingContext`。
4. 在数据库级 advisory lock 保护下执行版本化 migration。
5. 创建历史仓储、决策仓储、AI 托管引擎和 HTTP API。
6. 启动托管轮询、开奖扫描和 HTTP 服务。

连接、Ping 或 migration 任一步失败时，后端记录脱敏错误并退出，不允许自动退回内存历史。服务关闭时关闭 HTTP 服务、链客户端和数据库连接池。

## 数据表

### `schema_migrations`

记录已经执行的 migration 版本和执行时间。由于 MySQL DDL 会隐式提交，migration 必须设计为可重入、可在中断后安全重试；全部语句成功后才写入版本号。多个后端实例同时启动时使用 MySQL advisory lock 串行化 migration。

### `market_history`

字段：

- `contract_address VARCHAR(42)`：规范化的小写合约地址。
- `game_id BIGINT UNSIGNED`：链上博弈池 ID。
- `observed_at BIGINT UNSIGNED`：Unix 秒时间戳；链上点已按 AI 轮询间隔分桶。
- `yes_percent DECIMAL(9,6)`、`no_percent DECIMAL(9,6)`：规范化后合计为 100 的占比。
- `reserve_no VARBINARY(32) NULL`、`reserve_yes VARBINARY(32) NULL`：链上原始 uint256 的无符号大端字节；IPFS 种子允许为空。API 和模型边界再转换为十进制字符串。
- `source VARCHAR(16)`：仅允许 `chain` 或 `ipfs`。
- `created_at`、`updated_at`：数据库审计时间。

联合主键为 `(contract_address, game_id, observed_at)`。相同市场、相同时间桶执行 upsert；链上数据优先于 IPFS 种子，IPFS 不得覆盖已存在的链上储备和占比。为市场和时间倒序查询建立索引。

数据库保留完整历史，不按 `ai.history_max_points` 删除旧数据。该配置只限制 AI 每轮和默认 API 响应读取的最新点数，避免模型上下文无限增长；配置校验同时要求它不超过 API 上限 1000。

### `ai_decisions`

字段：

- 自增 ID。
- 合约地址、game ID、用户地址和决策时间。
- `decision_source`：`rule` 或 `model`。
- `action`：`buy_yes`、`buy_no` 或 `hold`。
- `confidence`、`reason` 和当时的历史点数。
- `outcome`：`pending`、`history_insufficient`、`invalid_reserves`、`hold`、`low_confidence`、`cooldown`、`traded` 或 `trade_failed`。
- `tx_hash` 和可脱敏的错误摘要。
- 创建和更新时间。

为市场时间、用户时间建立索引。历史不足和非法储备导致的后端规则 HOLD 也写入此表，使“没有调用模型、没有交易”的原因可查询。

## 仓储接口与组件边界

新增 MySQL 基础组件负责：打开数据库、连接池配置、Ping、migration 和关闭连接。

AI 托管包只依赖两个接口：

- 历史仓储：原子合并 IPFS 种子与当前链上点、查询市场最新历史。
- 决策仓储：创建待处理决策、完成决策结果、记录规则 HOLD。

MySQL 实现负责 SQL、事务、upsert 和类型转换；托管引擎继续负责链上读取、占比计算、历史门禁、模型调用与交易编排。HTTP 历史处理器只依赖只读历史接口，不直接使用数据库连接。

测试通过内存 fake 实现仓储接口，不要求本地 MySQL；SQL 层单独使用 mock 和可选集成测试验证。

## 托管数据流

每个托管任务的处理顺序：

1. 解密当前进程内的用户私钥并创建链客户端。
2. 读取 `getGameInfo`、IPFS 元数据与 `getGameExtraData`。
3. 按合约储备顺序 `NO, YES` 使用大整数/有理数计算 YES/NO 百分比。
4. 在一个仓储操作中写入有效 IPFS 种子并 upsert 当前链上时间桶点。
5. 从 MySQL 读取该市场最新 `ai.history_max_points` 个点，再按时间升序交给托管引擎。
6. 少于 `ai.history_min_points` 时写入 `rule/hold/history_insufficient`，不获取行情、不调用模型、不交易。
7. 储备无效时写入 `rule/hold/invalid_reserves`，不调用模型、不交易。
8. 达到门槛后获取黄金行情并调用模型。
9. 模型结果先创建 `pending` 决策记录。记录失败时立即返回错误，禁止交易。
10. HOLD、低置信度或冷却分别完成为相应 outcome。
11. 允许买入时发送交易；成功更新为 `traded` 和交易哈希，失败更新为 `trade_failed` 后返回原始交易错误。

历史读取、写入或规则 HOLD 记录失败时，本轮终止，不调用后续模型、不交易。运行期瞬时数据库错误由连接池处理；当前轮返回错误并由下一轮自然重试。

## 历史 API

新增：

```text
GET /api/gold/market-history?contract_address=...&game_id=...&limit=256
```

要求：

- 合约地址必须有效，game ID 必须为正整数。
- `limit` 默认使用 `ai.history_max_points`，必须为 `1..1000`。
- 仓储可以按倒序高效读取，但响应必须按 `observed_at` 升序排列。
- 响应包含时间、YES/NO 百分比、可选原始储备和数据来源。
- 参数错误返回 `400`，数据库错误返回 `503`，成功返回 JSON 和现有 CORS 头。
- API 只读，不提供客户端写历史的入口，防止前端模拟点污染 AI 数据。

本轮不修改 Android 调用。后续前端将折线图切换到此 API，并删除 `generateMockHistory`。

## 错误处理与安全

- MySQL 是必需依赖；不可用时服务拒绝启动。
- 所有数据库操作使用有截止时间的 context。
- DSN、密码、私钥不写日志、不进入 HTTP 响应、不写入测试 fixture 的真实值。
- 数据库用户只授予当前 schema 所需的 DDL/DML 权限。
- SQL 全部使用参数绑定；表名和列名只来自代码内常量。
- AI 决策审计记录创建失败时禁止发送交易。
- 交易已经广播但决策完成更新失败时记录高优先级结构化日志，并保留待处理记录供后续修复；不得重复发送交易。
- `managed_markets` 和私钥持久化不在本轮范围，避免引入长期密钥管理风险。

## Migration 与本地运行

仓库包含顺序编号的 SQL migration，并在 Go 二进制中嵌入，部署时无需额外复制 SQL 文件。migration 只前进，不在服务启动时自动回滚。

提供本地 MySQL 8 Docker Compose 配置、数据库初始化说明和示例 YAML。Compose 中仅使用开发占位密码，不复用生产凭据。

可选真实 MySQL 集成测试从专用测试 DSN 读取连接信息，只创建和删除独立测试数据库或表，不接触开发/生产数据。

## 测试与验收

自动化测试覆盖：

- YAML MySQL 配置读取、严格字段校验、连接池边界和错误脱敏。
- migration 顺序、重复执行幂等性和 advisory lock 行为。
- 历史 upsert、链上优先、IPFS 不覆盖链上、排序、limit 和 uint256 储备。
- 同一市场多个托管用户在一个时间桶只保留一条链上历史。
- 数据库读写失败时行情、模型和交易调用次数均为零。
- 强制 HOLD、模型 HOLD、低置信度、冷却、成功交易和交易失败的决策审计结果。
- 待处理决策创建失败时禁止交易；交易完成更新失败时不重复交易。
- 历史 API 的参数、默认/最大 limit、升序响应、CORS、`400` 和 `503`。
- 默认 `go test ./...` 不访问真实外网、不连接真实 MySQL、不调用真实 AI、不广播交易。
- `go vet ./...` 和 `go test -race ./internal/aimanaged` 通过。
- 可选 MySQL 8 集成测试验证真实 migration、完整 256 位储备、upsert 和并发唯一键语义。

## 后续工作

完成本后端阶段后：

1. Android 折线图读取后端历史 API。
2. 删除前端 `generateMockHistory` 随机曲线。
3. 根据实际数据量增加按时间归档或保留策略。
4. 如需重启后自动恢复托管，再单独设计外部钱包签名授权或稳定主密钥管理；不得直接把私钥明文写入 MySQL。
