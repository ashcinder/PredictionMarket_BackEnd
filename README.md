# PredictionMarket Backend

该服务负责 BrokerChain 黄金预测市场的数据缓存、AI 自动托管和到期裁决。配置统一从仓库根目录的 `config.yaml` 读取；`config.example.yaml` 提供不含真实密钥的完整模板。

## 启动前配置

1. 复制 `config.example.yaml` 为 `config.yaml`，填写钱包私钥、合约地址、MySQL 和需要启用的 AI 服务。
2. 启动本机 MySQL 8，并在 `mysql.dsn` 中填写本地数据库连接。连接池由 `max_open_connections`、`max_idle_connections` 和 `connection_max_lifetime_seconds` 控制。
3. AI 自动托管至少需要一个可用的投研模型账户；多 AI 裁决需要配置参与复核的模型以及最终裁定模型。API Key 无效、账户余额不足或 HTTP 402 都会使该轮保持失败或待重试，不会由本地 Supervisor 私钥代替 AI 服务鉴权。
4. 自动托管历史参数使用 `history_min_points` 和 `history_max_points`。正式运行前应确认链、数据库、IPFS 和 AI 服务分别可达。

后端启动命令为：

```bash
go run .
```

该命令会按当前配置监听市场，并可能执行交易或结算。只做代码验证时使用 `go test ./...`、`go vet ./...` 和 `go build ./...`，不要启动常驻服务。

## 本地 Redis 缓存

后端可以直接连接本机 Redis，不需要 Docker。默认配置为：

```yaml
redis:
  enabled: true
  address: "127.0.0.1:6379"
  password: ""
  db: 1
  key_prefix: "predictionmarket:cn-amm"
  operation_timeout_milliseconds: 300
  quote_ttl_seconds: 10
  public_data_ttl_seconds: 5
  research_ttl_seconds: 600
```

Redis 目前用于三类数据：

- 用户界面的黄金实时行情，缓存 10 秒。AI 自动托管、行情采样和开奖仍读取
  原始行情源，不使用该展示缓存。
- 博弈池列表、详情、不含用户持仓的链上池状态和份额历史，默认缓存 5 秒。
  写入博弈池、链状态或历史数据成功后会提升缓存版本，使旧数据立即失效。
- AI 投研结果，按照系统提示词与用户消息的 SHA-256 摘要缓存 10 分钟。
  Redis 不保存原始提示词；并发的相同请求会合并为一次上游模型调用。

macOS 可直接启动本地 Redis：

```bash
brew install redis
brew services start redis
redis-cli ping
```

`redis-cli ping` 应返回 `PONG`。Redis 不可用时，后端会记录警告并自动回退到
原有数据源，不会阻止 MySQL、Supervisor 或 HTTP API 启动。

CN-AMM 分支固定使用 MySQL 数据库 `predictionmarket_cn_amm`、Redis DB 1
以及 `predictionmarket:cn-amm` 前缀。运行配置会强制应用这组隔离参数，避免
误读原 CN/ENG 分支的数据。

## AMM 买入与卖出

当前合约支持在截止前买入或卖出 YES/NO 份额。卖出由链上
`quoteSellShares` 使用与买入相同的恒定乘积约束确定报价，再由
`sellShares(gameId, optionId, shareAmount, minAmountOut)` 执行。客户端默认
使用 1% 最低到账保护；报价恶化超过该范围时交易回滚，不会静默接受差价。

AI 自动托管使用“新增仓位 + 退出仓位”双信号：模型在同一次市场分析中同时
返回 `action`（`buy_yes`、`buy_no` 或 `hold`）和 `exit_action`
（`sell_yes`、`sell_no` 或 `hold`）。后端再结合每个用户的真实链上持仓，
优先减持被模型判断为高估的已有仓位；没有对应持仓时才考虑新增目标方向。
因此每个市场仍只调用一次 AI，不会随托管用户数量重复消耗 Token。

卖出比例由概率偏差、置信度、Kelly 风险偏好和风险标记共同决定，每轮限制为
对应持仓的 10%～50%。信息不足、信号冲突和高波动会降低减持上限，临近截止且
证据充分时会提高退出优先级。最终执行仍需通过概率一致性、最低优势、置信度、
冷却期、链上即时询价和 1% 滑点保护等确定性门控。

## Chainlink 结算信源

新建模板市场使用 Ethereum Chainlink Data Feed，不需要付费 Data Streams 订阅，也不依赖浏览器抓取。关键 YAML 字段如下：

```yaml
oracle:
  chainlink_rpc_urls:
    - "https://1rpc.io/eth"
    - "https://rpc.mevblocker.io"
    - "https://eth-mainnet.public.blastapi.io"
    - "https://rpc.eth.gateway.fm"
    - "https://eth-pokt.nodies.app"
  chainlink_xau_usd_feed: "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"
  chainlink_btc_usd_feed: "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"
  chainlink_eth_usd_feed: "0x5f4eC3Df9cbd43714FE2740f5E3616155c5b8419"
  chainlink_sol_usd_feed: "0x4ffC43a60e009B551865A93d232E33Fce9f01507"
  chainlink_bnb_usd_feed: "0x14e613AC84a31f709eadbdF89C6CC390fDc9540A"
  chainlink_poll_interval_seconds: 60
  chainlink_max_staleness_seconds: 43200
```

节点按顺序自动故障切换。边界证据采用 `Asia/Shanghai` 时区的 00:00，以及边界时刻之前最后一轮有效报价。若报价缺失、超过允许陈旧时间、格式异常或所有 RPC 均不可用，结果为 `INDETERMINATE` 并在后续轮次重试，不猜测 YES/NO。

## 可创建模板

新市场只允许以下六种 `rule_version: 2` 规则：

- `TYPE_PRICE`：比较观察期起止金价方向。
- `TYPE_RETURN_THRESHOLD`：判断黄金绝对涨跌幅是否达到阈值。
- `TYPE_PRICE_THRESHOLD`：判断截止金价是否达到目标价格。
- `TYPE_PRICE_RANGE`：判断截止金价是否位于指定区间。
- `TYPE_RELATIVE`：比较同一观察期内黄金与 BTC、ETH、SOL 或 BNB 的收益率。
- `TYPE_STREAK`：逐日验证黄金是否连续上涨或连续下跌。

观察期为北京时间整日，持续 1 至 4 天，起止边界必须落在周二至周六；连续涨跌窗口不能跨越周日或周一。旧市场仍可读取，但旧的成交量、技术指标、事件驱动、区间触及和盘中波动模板不再用于新建市场。

## 裁决流程

到期后，解析器先从 Chainlink 历史轮次或本地轮次缓存构造可审计证据，并执行确定性计算。随后 N-1 个 AI 独立复核规则、边界报价和计算结果；最后一个 AI 接收全部复核意见与原始证据，给出最终裁定。只有最终裁定与证据包完整时才发送链上结算交易，否则市场保持待裁决并继续轮询。

运行日志会记录边界时间、Feed 地址、轮次、源时间、价格、确定性计算、各复核模型意见和最终裁定，便于演示与审计。
