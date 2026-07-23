# 中文市场运行说明

中文市场与英文市场是两套相互隔离、可以同时运行的展示环境。它们共用本地 Supervisor 链节点，但使用不同的智能合约、数据库、后端端口和 Android 应用包名。

| 项目 | 英文市场（ENG） | 中文市场（CN） |
| --- | --- | --- |
| 前端目录 | `brokerwallet-ai-agent` | `brokerwallet-ai-agent-cn` |
| 后端目录 | `PredictionMarket` | `PredictionMarket-cn` |
| 后端端口 | `8081` | `8081` |
| 数据库 | `predictionmarket_local` | `predictionmarket_cn` |
| Android 包名 | `com.example.brokerfi` | `com.example.brokerfi.cn` |
| 智能合约 | ENG 原有合约 | `0xda550FdB040A10ff1f5467042aE3E7E13DF43F7F` |

## 中文市场合约

- 合约地址：`0xda550FdB040A10ff1f5467042aE3E7E13DF43F7F`
- 部署交易：`0x8a931cdfe024ce3d595f8dd333ff87b537d03e7f8495351d654a6d7218f8e8a9`
- 部署网络：本地 Supervisor EVM RPC

## 启动中文市场

1. 先启动现有的本地 Supervisor 链节点。
2. 进入 `PredictionMarket-cn`，运行 `go run .`。
3. 确认后端监听 `8081`，数据库为 `predictionmarket_cn`，合约地址为上述中文市场合约。
4. 使用 Android Studio 打开 `brokerwallet-ai-agent-cn`，构建并运行中文应用。

中文应用使用独立包名，因此可以与英文应用同时安装在同一台设备上。中文后端与英文后端共用 `8081` 端口，运行前按需要切换分支，不能同时启动两套后端。

## 隔离规则

- 不要把中文后端改回英文数据库，否则两套展示数据会混合。
- 中文前端与英文前端均连接 `8081`；启动应用前必须确保正在运行的是对应语言分支的后端。
- 两套市场虽然连接同一个本地链节点，但合约地址不同，链上博弈池与交易互不混用。
- `predictionmarket_cn` 保留完整表结构，首次正式展示前所有业务表保持为空。
