# Prediction Market Simulator

这是一个独立的测试数据模拟器，用来模拟多个用户创建博弈池、给新池注入初始流动性、再由多个随机账户参与购买。

默认不会直接写数据库，也不会直接上链。它会先生成一份可检查的计划文件，你确认后再决定这批数据是只落库，还是发真实链上交易。

## 两步流程

第一步：生成预览计划。

```bash
cd simulator
go run ./cmd/simulator -config config.yaml
```

默认配置是：

```yaml
runtime:
  mode: "preview"
  plan_file: "out/simulator-plan.json"
```

运行后会打印本次随机生成的账户、博弈池、交易，并保存到 `out/simulator-plan.json`。

第二步：你看完计划后，再选择执行方式。

只写数据库，不上链：

```yaml
runtime:
  mode: "execute"
  on_chain: false
  plan_file: "out/simulator-plan.json"
mysql:
  dsn: "..."
```

真实上链并同步数据库：

```yaml
runtime:
  mode: "execute"
  on_chain: true
  approve_on_chain: true
  plan_file: "out/simulator-plan.json"
chain:
  private_key: "..."
mysql:
  dsn: "..."
```

`approve_on_chain: true` 是额外确认开关。没有这个开关，即使 `on_chain: true`，模拟器也不会允许执行链上交易。

## 执行模式

`runtime.mode: preview`

只生成并展示计划，不写数据库，不发链上交易。计划文件里保存了随机账户、博弈池和交易金额，所以后续执行的是你已经看过的同一批数据。

注意：计划文件包含模拟账户私钥，只用于本地测试，不要提交或分享。`out/` 已经被 `.gitignore` 忽略。

`runtime.mode: execute` + `runtime.on_chain: false`

只写数据库，不发链上交易。模拟器会读取你已经预览过的计划，按计划创建博弈池、购买 YES/NO，并把结果写入：

- `gold_games`
- `gold_chain_states`
- `gold_user_positions`
- `gold_trades`
- `gold_price_history`
- `market_history`

这适合快速给前端和后端接口准备展示数据。注意：这种模式下链上合约并不知道这些 `game_id`。

`runtime.mode: execute` + `runtime.on_chain: true`

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

是否允许运行。设为 `false` 且处于 `execute` 模式时会直接停止，防止误执行。

`runtime.mode`

`preview` 表示只生成计划文件；`execute` 表示读取计划文件并执行。

`runtime.on_chain`

执行阶段是否发真实链上交易。`false` 是 DB-only 模拟，`true` 是真实调用合约。

`runtime.plan_file`

预览阶段写入、执行阶段读取的计划文件。执行阶段不会重新随机生成数据。

`runtime.approve_on_chain`

上链执行的确认开关。只有 `mode: execute`、`on_chain: true`、`approve_on_chain: true` 同时满足时，才会发链上交易。

`runtime.dry_run`

旧版预览开关。现在推荐使用 `runtime.mode: preview`。

`chain.private_key`

上链执行模式需要。这个账户会先给随机模拟账户转测试币，随机账户再各自创建和购买。

`chain.contract_address`

预测市场合约地址。

`chain.rpc_url`

本地或普通 EVM RPC 地址，`use_broker_chain: false` 时使用。

`chain.broker_chain_url` / `chain.use_broker_chain`

是否改用 BrokerChain 接口发交易和读链。开启后仍会使用 `private_key` 给随机账户签名。

`mysql.dsn`

数据库连接串。`mode: execute` 时必须填写。

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
