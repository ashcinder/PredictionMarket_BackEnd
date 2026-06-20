# PredictionMarket 后端配置指南

Go 后端只读取项目根目录的 `config.yaml`。真实配置包含钱包私钥与 AI API Key，已通过 `.gitignore` 排除；请勿提交或分享该文件。

## 快速开始

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，至少替换以下两项：

- `chain.private_key`：后端开奖钱包私钥。
- `ai.api_key`：与 `ai.base_url` 对应的模型 API Key。

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
| `ipfs` | `gateway` | IPFS HTTP(S) 网关 |
| `sentinel` | `poll_interval_seconds` | 开奖扫描间隔，必须大于 0 |
| `sentinel` | `resolve_delay_seconds` | 开奖前延迟，不能为负数 |
| `ai` | `api_key` | 必填，模型服务 API Key |
| `ai` | `base_url` | 模型 Chat Completions HTTP(S) 地址 |
| `ai` | `model` | 模型名称 |
| `ai` | `poll_interval_seconds` | AI 托管扫描间隔，必须大于 0 |
| `ai` | `buy_amount_bkc` | 单次自动买入 BKC 数量，必须大于 0 |
| `ai` | `confidence_min` | 自动交易最低置信度，范围为 0 到 1 |

配置采用严格校验：未知字段、非法地址、非法 URL 或无效交易参数都会阻止服务启动。后端不再读取 XML，也不使用环境变量覆盖 YAML。

## 安全测试 AI 托管

```bash
go test ./...
```

测试使用内存存储、模拟 AI、模拟行情和模拟链客户端，不访问真实外网，也不会广播真实交易。只有启动后端并启用托管时，程序才会使用 `config.yaml` 中的真实服务和资金参数。

## 常见错误

- `chain.private_key is required`：仍在使用模板私钥。
- `ai.api_key is required`：仍在使用模板 API Key。
- `chain.rpc_url must be an HTTP(S) URL`：关闭 BrokerChain 后没有填写有效 RPC URL。
- `config.yaml` 不存在：执行 `cp config.example.yaml config.yaml` 后再编辑。
