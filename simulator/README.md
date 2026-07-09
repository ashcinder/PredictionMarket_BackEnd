# Prediction Market Simulator

这是一个独立的测试数据模拟器，用来模拟多个用户创建博弈池、给新池注入初始流动性、再由多个随机账户参与购买。

它不依赖后端服务启动，直接按 `config.yaml` 的配置执行：

```bash
cd simulator
go run ./cmd/simulator -config config.yaml
```

## 两种运行模式

`runtime.on_chain: false`

只写数据库，不发链上交易。模拟器会自己生成随机账户、随机创建博弈池、随机购买 YES/NO，并把结果写入：

- `gold_games`
- `gold_chain_states`
- `gold_user_positions`
- `gold_trades`
- `gold_price_history`
- `market_history`

这适合快速给前端和后端接口准备展示数据。注意：这种模式下链上合约并不知道这些 `game_id`。

`runtime.on_chain: true`

先发真实链上交易，再同步数据库。流程是：

1. 用 `chain.private_key` 对应的钱包给随机生成的模拟账户打测试币。
2. 随机账户调用 `createGame(ipfsCID, duration)` 创建博弈池。
3. 随机账户调用 `buyShares(gameId, optionId)` 购买 YES/NO。
4. 读取链上 `getGameInfo` / `getGameExtraData`，把真实链上状态同步到数据库。

## 真实用户行为

默认 `scenario.type: create_and_trade`，不是只买现有池。它会模拟：

- 随机账户 A 创建博弈池
- 随机账户 B/C/D... 买入该池
- 多个池循环执行
- 创建者是否也参与购买由 `trade.creator_also_trades` 控制

如果只想压测已有池，可以改成：

```yaml
scenario:
  type: "trade_existing"
  existing_game_ids: [1, 2, 3]
```

`trade_existing` 真正执行时需要 `runtime.on_chain: true`，因为已有池的储备、份额和价格应以链上状态为准；dry-run 模式可以只预览计划。

## 支持的博弈池类型

这些类型来自当前前端创建池代码：

- `TYPE_PRICE`
- `TYPE_VOLATILITY`
- `TYPE_VOLUME`
- `TYPE_TECHNICAL`
- `TYPE_TOUCH`
- `TYPE_RELATIVE`
- `TYPE_PRICE_THRESHOLD`
- `TYPE_EVENT`

模拟器会根据类型随机生成标题、条件、选项文案和 metadata。合约本身只保存 `ipfsCID` 和时间，类型信息属于池的 metadata/数据库展示层。

## 主要配置

`runtime.enabled`

是否允许运行。设为 `false` 且不是 `dry_run` 时会直接停止，防止误执行。

`runtime.on_chain`

是否发真实链上交易。`false` 是 DB-only 模拟，`true` 是真实调用合约。

`runtime.dry_run`

只打印计划，不写数据库，不发交易。第一次运行建议保持 `true`。

`chain.private_key`

上链模式需要。这个账户会先给随机模拟账户转测试币，随机账户再各自创建和购买。

`chain.contract_address`

预测市场合约地址。

`chain.rpc_url`

本地或普通 EVM RPC 地址，`use_broker_chain: false` 时使用。

`chain.broker_chain_url` / `chain.use_broker_chain`

是否改用 BrokerChain 接口发交易和读链。开启后仍会使用 `private_key` 给随机账户签名。

`mysql.dsn`

数据库连接串。`dry_run: false` 时必须填写。

`scenario.market_count`

本次随机创建多少个博弈池。

`scenario.participants`

随机模拟账户数量。

`scenario.trades_per_market_min/max`

每个博弈池会随机产生多少笔购买交易。

`market.types`

允许随机创建的池类型。留空会使用全部 8 种类型。

`market.initial_liquidity_min/max_bkc`

每个新池初始流动性的随机范围，单位是 BKC。

`market.duration_min/max_seconds`

每个新池持续时间的随机范围，单位是秒。

`trade.buy_min/max_bkc`

每笔购买金额的随机范围，单位是 BKC。

`trade.creator_also_trades`

创建者是否也可以参与购买自己创建的池。

`timing.pause_seconds`

每笔交易之间暂停多久，避免请求打得太密。

`timing.timeout_seconds`

整次模拟运行的超时时间。

## YES/NO 编号约定

现有前端、后端和合约都使用：

- `option_id = 0` 表示 YES
- `option_id = 1` 表示 NO

但链上 `getGameExtraData` 返回的储备数组顺序是 `[reserveNO, reserveYES]`。模拟器内部已经按这个顺序换算价格，DB-only 模式也按合约的恒定乘积公式模拟买入后的储备和份额变化。
