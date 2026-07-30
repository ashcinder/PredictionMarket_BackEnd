# CN-AMM 中文市场运行说明

CN 与 CN-AMM 共用本地 Supervisor 链节点和项目目录，但通过 Git 分支、智能合约、MySQL 数据库与 Redis 命名空间隔离。两套版本都使用 `8081`，因此按需切换运行，不能同时启动后端。

| 项目 | 原中文市场（CN） | 中文 AMM 市场（CN-AMM） |
| --- | --- | --- |
| 前端 | `brokerwallet-ai-agent` / `CN` 分支 | `brokerwallet-ai-agent` / `CN-AMM` 分支 |
| 后端 | `PredictionMarket` / `CN` 分支 | `PredictionMarket` / `CN-AMM` 分支 |
| 后端端口 | `8081` | `8081` |
| 数据库 | `predictionmarket_cn` | `predictionmarket_cn_amm` |
| Redis | 原分支配置 | DB 1 / `predictionmarket:cn-amm` |
| Android 包名 | `com.example.brokerfi` | `com.example.brokerfi.cn` |
| 智能合约 | `0xda550FdB040A10ff1f5467042aE3E7E13DF43F7F` | `0x2C26fF6eA7Ec23C09f64A62de5C060c0D00208D1` |

## 中文市场合约

- 合约地址：`0x2C26fF6eA7Ec23C09f64A62de5C060c0D00208D1`
- 部署交易：`0xaea8e278567b0b31fc7d6d1f09295d82ca4d935afaf80e773395d94cae1985af`
- 部署网络：本地 Supervisor EVM RPC

## 启动中文市场

1. 先启动现有的本地 Supervisor 链节点。
2. 确认本机 MySQL 与 Redis 已启动。
3. 进入 `PredictionMarket` 的 `CN-AMM` 分支，运行 `go run .`。
4. 确认后端监听 `8081`，数据库为 `predictionmarket_cn_amm`，合约地址为上述 CN-AMM 合约。
5. 使用 Android Studio 打开 `brokerwallet-ai-agent` 的 `CN-AMM` 分支，构建并运行中文应用。

两个分支共用应用包名和 `8081` 端口，运行前必须确认前后端都处于同一个分支版本。

## 隔离规则

- 不要把中文后端改回英文数据库，否则两套展示数据会混合。
- 中文前端与英文前端均连接 `8081`；启动应用前必须确保正在运行的是对应语言分支的后端。
- 两套市场虽然连接同一个本地链节点，但合约地址不同，链上博弈池与交易互不混用。
- `predictionmarket_cn_amm` 保留完整表结构，首次正式展示前所有业务表保持为空。
- CN-AMM 合约支持截止前按恒定乘积约束卖出份额，客户端默认使用 1% 最低到账保护。
- 创建者的初始流动性会铸造成首批 LP 份额；其他用户也可按当前储备比例
  注入流动性，并按 LP 占比取回资产及累计的 1% 交易费。
- 博弈池运行期间，创建者的首批 LP 份额保持锁定；开奖或退款后才可取回。
- 运行中的池不得取回最后一份 LP，以免储备清空；结算后 LP 按占比领取
  胜方储备与尚未领取的交易费。
- AI 自动托管采用新增仓位与退出仓位双信号；后端根据用户实际持仓执行
  `BUY YES`、`BUY NO`、`SELL YES`、`SELL NO` 或 `HOLD`，并将卖出同步到
  MySQL 交易记录、Redis 公共缓存失效流程和 AI 决策审计。
