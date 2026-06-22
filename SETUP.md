# PredictionMarket 后端配置指南

Go 后端只读取项目根目录的 `config.yaml`。真实配置包含钱包私钥与 AI API Key，已通过 `.gitignore` 排除；请勿提交或分享该文件。

## 快速开始

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，至少替换以下两项：

- `chain.private_key`：后端开奖钱包私钥。
- `ai.api_key`：与 `ai.base_url` 对应的模型 API Key。
- `mysql.dsn`：MySQL 8 连接串，包含数据库用户、密码、地址和数据库名。

本地可以先启动仓库提供的 MySQL 8：

```bash
docker compose -f docker-compose.mysql.yml up -d
docker compose -f docker-compose.mysql.yml ps
```

开发容器对应的 DSN 是：

```text
prediction:prediction-dev-password@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC
```

这些是本地开发凭据，生产部署必须替换。

随后启动：

```bash
./start.sh
```

也可以直接运行 `go run main.go`。直接运行会连接 YAML 中配置的真实链、行情、IPFS 和 AI 服务。

## 配置项

| 分组 | 配置项 | 说明 |
| --- | --- | --- |
| `chain` | `private_key` | 必填，开奖与签名钱包私钥 |
| `chain` | `contract_address` | 必填，预测市场合约地址 |
| `chain` | `rpc_url` | `use_broker_chain: false` 时必填 |
| `chain` | `broker_chain_url` | BrokerChain HTTP(S) 地址 |
| `chain` | `use_broker_chain` | `true` 使用 BrokerChain，`false` 使用 RPC |
| `server` | `http_listen` | AI 托管 HTTP 服务监听地址 |
| `mysql` | `dsn` | 必填，MySQL 8 DSN；真实密码只写入被忽略的 `config.yaml` |
| `mysql` | `max_open_connections` | 最大连接数，必须大于 0 |
| `mysql` | `max_idle_connections` | 最大空闲连接数，必须大于 0 且不超过最大连接数 |
| `mysql` | `connection_max_lifetime_seconds` | 连接最长复用时间，必须大于 0 |
| `ipfs` | `gateway` | IPFS HTTP(S) 网关 |
| `oracle` | `gold_api_url` | Gold API 行情 HTTP(S) 地址 |
| `oracle` | `sina_url` | 新浪黄金行情 HTTP(S) 地址 |
| `oracle` | `sina_referer` | 新浪接口要求的 Referer 地址 |
| `oracle` | `user_agent` | 行情请求 User-Agent |
| `oracle` | `request_timeout_seconds` | 行情请求超时，必须大于 0 |
| `sentinel` | `poll_interval_seconds` | 开奖扫描间隔，必须大于 0 |
| `sentinel` | `resolve_delay_seconds` | 开奖前延迟，不能为负数 |
| `ai` | `api_key` | 必填，模型服务 API Key |
| `ai` | `base_url` | 模型 Chat Completions HTTP(S) 地址 |
| `ai` | `model` | 模型名称 |
| `ai` | `poll_interval_seconds` | AI 托管扫描间隔，必须大于 0 |
| `ai` | `buy_amount_bkc` | 单次自动买入 BKC 数量，必须大于 0 |
| `ai` | `confidence_min` | 自动交易最低置信度，范围为 0 到 1 |
| `ai` | `history_min_points` | 调用模型前要求的真实历史点数，推荐为 3，必须大于 0 |
| `ai` | `history_max_points` | 进程内每个市场最多保留的历史点数，推荐为 256，且不能小于最小点数 |

配置采用严格校验：未知字段、非法地址、非法 URL、MySQL DSN 或无效交易参数都会阻止服务启动。MySQL 连接、Ping 或 migration 失败时后端拒绝启动，不会退回易丢失的内存历史。后端不再读取 XML，也不使用环境变量覆盖 YAML。

## 查询真实市场历史

```bash
curl "http://127.0.0.1:8081/api/gold/market-history?contract_address=0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c&game_id=1&limit=256"
```

接口只读，返回按时间升序排列的 YES/NO 占比、可选原始储备和数据来源。

## 安全测试 AI 托管

```bash
go test ./...
```

测试使用内存存储、模拟 AI、模拟行情和模拟链客户端，不访问真实外网，也不会广播真实交易。只有启动后端并启用托管时，程序才会使用 `config.yaml` 中的真实服务和资金参数。

## 常见错误

- `chain.private_key is required`：仍在使用模板私钥。
- `ai.api_key is required`：仍在使用模板 API Key。
- `chain.rpc_url must be an HTTP(S) URL`：关闭 BrokerChain 后没有填写有效 RPC URL。
- `mysql.dsn is required`：仍在使用 MySQL 占位密码或没有填写 DSN。
- `init mysql failed`：MySQL 未启动、账号密码错误或 migration 失败。
- `config.yaml` 不存在：执行 `cp config.example.yaml config.yaml` 后再编辑。
