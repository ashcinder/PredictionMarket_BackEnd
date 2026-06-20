# AI 托管与 YAML 配置设计

## 目标与范围

本次变更只覆盖 Go 后端，不修改 `agent/` 下的 Android 代码。后端所有运行配置统一由根目录的 `config.yaml` 提供，包括钱包私钥、AI API Key、服务地址、RPC/BrokerChain/IPFS URL、合约地址、轮询参数和 AI 交易参数。

真实 `config.yaml` 仅保存在本地并加入 `.gitignore`；仓库只提交不含真实密钥的 `config.example.yaml`。测试不会请求真实 AI、行情、IPFS、RPC 或 BrokerChain，也不会广播真实链上交易。

## 配置结构

YAML 使用按职责分组的结构：

```yaml
chain:
  private_key: "replace-with-wallet-private-key"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
  broker_chain_url: "https://dash.broker-chain.com:443/"
  use_broker_chain: true

server:
  http_listen: ":8081"

ipfs:
  gateway: "http://127.0.0.1:8080/ipfs/"

sentinel:
  poll_interval_seconds: 30
  resolve_delay_seconds: 5

ai:
  api_key: "replace-with-ai-api-key"
  base_url: "https://api.deepseek.com/chat/completions"
  model: "deepseek-chat"
  poll_interval_seconds: 120
  buy_amount_bkc: "10"
  confidence_min: 0.70
```

`config.example.yaml` 显式列出每个配置项，避免关键 URL 或交易参数继续藏在代码默认值和环境变量中。`config.yaml` 是唯一运行配置源，不再读取 XML 或 `DEEPSEEK_API_KEY`、`OPENAI_API_KEY`、`AI_BASE_URL` 等环境变量。

加载器把嵌套 YAML 转换为现有运行时 `Config`，以减少业务代码改动。钱包私钥、AI API Key、合约地址、AI Base URL 和模型名为必填项；RPC URL 在 `use_broker_chain: false` 时必填。加载时还验证 URL、正数轮询间隔、非负开奖延迟、`0..1` 置信度以及正数 BKC 买入金额。IPFS 网关统一补齐末尾 `/`。

## 组件与数据流

`internal/config` 改为解析 `config.yaml`。`main.go` 的启动流程保持不变，继续把同一个运行时配置传给开奖哨兵与 AI 托管引擎。

`NewAIClient` 改从 `cfg.AIAPIKey` 读取密钥，模型请求仍使用 `cfg.AIBaseURL` 和 `cfg.AIModel`。AI 托管数据流保持：HTTP 接口启用托管并加密保存用户私钥，轮询引擎读取池子和行情，模型返回 `hold`、`buy_yes` 或 `buy_no`，达到置信度且通过冷却检查后才调用链客户端发送交易。

为了在无真实网络和资金的情况下测试完整决策路径，托管引擎为链客户端工厂、元数据读取、行情读取和 AI 决策增加最小接口。生产构造器注入现有真实实现；测试构造器注入内存模拟实现。业务规则不改变。

## 错误处理与安全

配置文件不存在、YAML 无法解析、必填密钥仍为示例值或字段校验失败时，服务拒绝启动并返回包含字段名的错误，但不打印密钥内容。日志可以输出非敏感 URL、模型名和监听地址，不输出钱包私钥、用户托管私钥或 AI API Key。

AI 托管接口现有的私钥与地址匹配校验、内存加密和非 TLS 警告保留。模拟测试只使用临时生成的测试私钥。真实 `config.yaml`、旧 `config.xml` 和托管密钥材料均不提交到 Git。

## 测试与验收

实施前的基线测试暴露了两个既有的开奖条件解析缺陷：成交量条件把 `大于 (Above) 100` 整段交给浮点解析，技术指标条件则从第一个括号前误取 `RSI`，因此两个阈值都无法进入比较逻辑。修复增加一个共用比较阈值解析器：从“大于/小于/等于”之后开始，跳过可选英文括号标记，再读取数字。成交量和技术指标分支共用该解析器，并以现有失败用例及纯中文格式用例作回归验证；比较语义和模拟值保持不变。

配置测试覆盖：完整 YAML 成功加载、缺少文件、错误 YAML、缺少必填密钥、RPC 模式缺少 URL、非法 URL、非法间隔/置信度/买入金额，以及 IPFS URL 规范化。

AI 托管测试覆盖：

- POST 启用托管时验证私钥与用户地址，并能通过 GET 查询状态；POST 禁用后状态关闭。
- 模拟 AI HTTP 服务验证 Authorization、模型和请求体，并返回可解析的交易决策。
- `hold` 或低置信度决策不调用发送交易。
- 高置信度 `buy_yes`/`buy_no` 只发送一次符合配置金额的模拟交易，并记录交易结果。
- 模拟依赖错误会记录任务错误，测试过程不访问外网。

验收命令为 `go test ./...`、`go vet ./...` 和一次使用无真实密钥测试配置的启动失败/配置校验检查。启动脚本和 `SETUP.md` 同步改为 YAML 操作说明。

## 迁移结果

删除 XML 配置解析与 XML 启动提示；不保留 XML/环境变量兼容层，避免出现多个配置真相源。已有使用者需从 `config.example.yaml` 复制生成本地 `config.yaml`，填入真实钱包私钥和 AI API Key 后启动。
