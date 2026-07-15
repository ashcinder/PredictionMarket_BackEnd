# PredictionMarket Backend

该服务负责 BrokerChain 黄金预测市场的数据缓存、AI 自动托管和到期裁决。配置统一从仓库根目录的 `config.yaml` 读取；`config.example.yaml` 提供不含真实密钥的完整模板。

## 启动前配置

1. 复制 `config.example.yaml` 为 `config.yaml`，填写钱包私钥、合约地址、MySQL 和需要启用的 AI 服务。
2. 使用 `docker-compose.mysql.yml` 启动 MySQL 8，或在 `mysql.dsn` 中填写现有数据库连接。连接池由 `max_open_connections`、`max_idle_connections` 和 `connection_max_lifetime_seconds` 控制。
3. AI 自动托管至少需要一个可用的投研模型账户；多 AI 裁决需要配置参与复核的模型以及最终裁定模型。API Key 无效、账户余额不足或 HTTP 402 都会使该轮保持失败或待重试，不会由本地 Supervisor 私钥代替 AI 服务鉴权。
4. 自动托管历史参数使用 `history_min_points` 和 `history_max_points`。正式运行前应确认链、数据库、IPFS 和 AI 服务分别可达。

后端启动命令为：

```bash
go run .
```

该命令会按当前配置监听市场，并可能执行交易或结算。只做代码验证时使用 `go test ./...`、`go vet ./...` 和 `go build ./...`，不要启动常驻服务。

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
