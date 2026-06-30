、# DApp 三层数据交互架构 — API 对齐文档

> **版本**: v1.0  
> **日期**: 2026-06-26  
> **目的**: 明确 DApp（Android）与 IPFS、区块链、Go 后端数据库的交互边界，方便前后端统一对齐 API。

---

## 目录

1. [架构概览](#1-架构概览)
2. [三层职责划分](#2-三层职责划分)
3. [IPFS 交互 API](#3-ipfs-交互-api)
4. [区块链交互 API](#4-区块链交互-api)
5. [Go 后端数据库 API](#5-go-后端数据库-api)
6. [数据流图](#6-数据流图)
7. [错误处理与回退策略](#7-错误处理与回退策略)
8. [性能对比](#8-性能对比)

---

## 1. 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                       DApp (Android)                             │
│  ┌───────────────┐  ┌────────────────┐  ┌───────────────────┐  │
│  │   ViewModel   │  │  GoldMarket    │  │   AI Agent        │  │
│  │   (UI State)  │──│  Repository    │──│   (DeepSeek)      │  │
│  └───────────────┘  └───────┬────────┘  └───────────────────┘  │
│                              │                                    │
│          ┌───────────────────┼───────────────────┐               │
│          ▼                   ▼                   ▼               │
│  ┌──────────────┐  ┌─────────────────┐  ┌──────────────────┐   │
│  │ PinataClient │  │BrokerChainClient│  │ BackendApiClient │   │
│  │   (IPFS)     │  │  (Blockchain)   │  │   (Go Backend)   │   │
│  └──────┬───────┘  └───────┬─────────┘  └────────┬─────────┘   │
└─────────┼──────────────────┼─────────────────────┼──────────────┘
          │                  │                     │
          ▼                  ▼                     ▼
   ┌──────────┐     ┌──────────────┐     ┌─────────────────┐
   │   IPFS   │     │  Blockchain  │     │  Go Backend     │
   │  (本地)  │     │ (BrokerChain)│     │  (PostgreSQL)   │
   └──────────┘     └──────────────┘     └─────────────────┘
```

### 核心设计原则

| 操作 | 策略 | 原因 |
|------|------|------|
| **读** | 后端 DB 优先 → IPFS/链上回退 | 减少链交互时延，DB 查询毫秒级响应 |
| **写** | IPFS → 链上 → 后端 DB 同步 | 保证数据不可篡改（链上+IPFS），同时加速后续读取（DB 缓存） |

---

## 2. 三层职责划分

### 2.1 IPFS（去中心化内容存储）

**职责**：存储不可变的内容数据

| 数据类型 | 说明 | 更新频率 |
|----------|------|----------|
| 图片（头像） | 博弈池图标/宣传图 | 创建时写入，不可变 |
| 元数据 JSON | 标题、条件、选项名、详细说明 | 创建时写入，不可变 |
| 历史价格 JSON | 价格走势数据（可选，历史快照） | 定期快照 |

**关键特性**：
- 内容寻址（CID），数据不可篡改
- 去中心化，不依赖单一服务器
- 读取速度取决于网络和网关

### 2.2 区块链（不可篡改账本）

**职责**：存储合约状态和资金

| 数据类型 | 说明 | 更新频率 |
|----------|------|----------|
| 合约状态 | totalPool, isResolved, winningOption, deadlineSec | 每笔交易后更新 |
| 储备金 | virtualReserves（合约返回顺序为 `[reserveNO, reserveYES]`） | 每笔买卖后更新 |
| 用户份额 | myShares (每个用户的 YES/NO 份额) | 每笔买卖后更新 |
| 交易记录 | 所有 buy/sell/claim/create 交易 | 实时 |

**关键特性**：
- 不可篡改，提供最终确定性
- 读操作（eth_call）有网络延迟（~200-500ms）
- 写操作（eth_sendTransaction）需等待区块确认（~2-30s）

### 2.3 Go 后端数据库（快速缓存 + 业务数据）

**职责**：缓存链上/IPFS 数据，存储业务扩展数据

| 数据类型 | 说明 | 更新频率 |
|----------|------|----------|
| 游戏元数据缓存 | 从 IPFS 同步的 desc/condition/avatarUrl 等 | 创建时同步 |
| 链上状态缓存 | 从链上定时同步的 totalPool/reserves/shares | 定时刷新 + 交易后主动更新 |
| 历史价格数据 | 每次交易时的 YES/NO 价格快照 | 每笔交易后写入 |
| AI 托管配置 | 用户的 AI 托管开关设置 | 用户操作时写入 |
| 交易记录 | 用户买卖记录（便于快速查询） | 每笔交易后写入 |

**关键特性**：
- 毫秒级查询响应
- 支持复杂 SQL 查询（排序、筛选、分页）
- 缓存可能滞后于链上真实状态（标注 `updated_at` 时间）

---

## 3. IPFS 交互 API

### 3.1 基础配置

```
IPFS API 地址:  http://10.0.2.2:5001/api/v0/add     (写入)
IPFS 网关地址:  http://10.0.2.2:8080/ipfs/{cid}      (读取)
```

> **注意**：`10.0.2.2` 是 Android 模拟器访问宿主机的固定 IP。生产环境需替换为实际 IPFS 节点地址。

### 3.2 上传文件（写入）

**端点**: `POST http://{ipfs-node}:5001/api/v0/add`

**请求格式**: `multipart/form-data`

```
Content-Type: multipart/form-data; boundary={boundary}

--{boundary}
Content-Disposition: form-data; name="file"; filename="{filename}"
Content-Type: {mime-type}

{file-bytes}
--{boundary}--
```

**响应格式**:
```json
{
    "Name": "avatar.png",
    "Hash": "QmXxx...xxx",
    "Size": "12345"
}
```

**DApp 调用场景**:

| 场景 | 文件类型 | Content-Type | 文件名 |
|------|----------|-------------|--------|
| 创建博弈池 - 上传图片 | PNG/JPEG 图片 | `image/png` | `avatar.png` |
| 创建博弈池 - 上传元数据 | JSON 文本 | `application/json` | `metadata.json` |

**DApp 代码位置**: `PinataClient.uploadFileToIPFS()` / `PinataClient.uploadJsonToIPFS()`

### 3.3 下载文件（读取）

**端点**: `GET http://{ipfs-gateway}:8080/ipfs/{cid}`

**响应**: 原始文件内容（JSON 字符串或图片二进制）

**DApp 调用场景**:

| 场景 | 说明 | 优先级 |
|------|------|--------|
| 博弈池详情页 - 加载元数据 | 获取 desc/condition/optionNames 等 | 回退路径（后端 DB 优先） |
| 博弈池列表 - 加载图片 | 加载 avatarUrl 对应的图片 | 直接读取（Glide 加载） |
| 博弈池列表 - 加载元数据 | 批量获取所有博弈池的标题 | 回退路径（后端 DB 优先） |

**DApp 代码位置**: `PinataClient.downloadJsonFromIPFS()` / `Glide.with().load(IPFS_GATEWAY + cid)`

### 3.4 IPFS 数据格式：元数据 JSON

**文件名**: `metadata.json`

**Schema**:
```json
{
    "desc": "2026-07-01 黄金价格 上涨",
    "condition": "黄金价格在 2026-07-01 相对基准 上涨 (Price Up)",
    "avatarUrl": "QmAvatar123...",
    "detailedInfo": "纽约金 COMEX 期货合约价格为准，...",
    "optionYES": "达成 (YES)",
    "optionNO": "未达成 (NO)",
    "history": [
        {
            "t": 1719878400,
            "y": 65.5,
            "n": 34.5
        }
    ]
}
```

**字段说明**:
| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `desc` | string | 是 | 博弈池标题 |
| `condition` | string | 是 | 结算判定逻辑 |
| `avatarUrl` | string | 否 | 图片 IPFS CID（空字符串表示无图片） |
| `detailedInfo` | string | 否 | 详细说明/Markdown 内容 |
| `optionYES` | string | 是 | 选项 YES 的显示名称 |
| `optionNO` | string | 是 | 选项 NO 的显示名称 |
| `history` | array | 否 | 历史价格快照 |

---

## 4. 区块链交互 API

### 4.1 基础配置

```
BrokerChain 服务端:  https://dash.broker-chain.com:440/
本地 RPC 节点:      由用户配置（开发调试用）
```

### 4.2 读取合约状态（eth_call）

**端点**: `POST https://dash.broker-chain.com:440/eth_call`

**请求格式**:
```json
{
    "PublicKey": "0x04...",
    "RandomStr": "uuid-v4",
    "To": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
    "data": "0x... (ABI encoded)",
    "value": "0x0",
    "Sign1": "0x...",
    "Sign2": "0x..."
}
```

**响应格式**:
```json
{
    "result": "0x... (ABI encoded return data)"
}
```

### 4.3 合约方法列表

#### 4.3.1 getGameInfo (读取单个博弈池)

| 项目 | 内容 |
|------|------|
| **方法签名** | `getGameInfo(uint256 gameId)` |
| **参数** | `gameId`: 博弈池 ID |
| **返回** | `(string ipfsCID, uint256 totalPool, bool isResolved, uint8 winningOption, uint256 deadlineSec, bool isRefunded)` |
| **DApp 调用场景** | 博弈池详情页加载 |
| **DApp 代码位置** | `GoldMarketRepository.getGameInfo()` — 回退路径 |

#### 4.3.2 getGameExtraData (读取用户持仓)

| 项目 | 内容 |
|------|------|
| **方法签名** | `getGameExtraData(uint256 gameId, address user)` |
| **参数** | `gameId`: 博弈池 ID, `user`: 用户地址 |
| **返回** | `(uint256[] reserves, uint256[] shares)` — reserves[0]=reserveNO, reserves[1]=reserveYES |
| **DApp 调用场景** | 博弈池详情页（用户份额/价格） |
| **DApp 代码位置** | `GoldMarketRepository.getGameInfo()` — 回退路径 |

#### 4.3.3 getAllGames (批量读取所有博弈池)

| 项目 | 内容 |
|------|------|
| **方法签名** | `getAllGames()` |
| **参数** | 无 |
| **返回** | `(uint256[] ids, string[] ipfsCIDs, uint256[] totalPools, uint256[] deadlines, bool[] isResolved, bool[] isRefunded, uint8[] winningOptions)` |
| **DApp 调用场景** | 博弈池列表页加载 |
| **DApp 代码位置** | `GoldMarketRepository.getAllGamesInfo()` — 回退路径 |

#### 4.3.4 getAllGamesExtraData (批量读取用户持仓)

| 项目 | 内容 |
|------|------|
| **方法签名** | `getAllGamesExtraData(address user)` |
| **参数** | `user`: 用户地址 |
| **返回** | `(uint256[] resNO, uint256[] resYES, uint256[] myYES, uint256[] myNO)` |
| **DApp 调用场景** | 博弈池列表页（用户份额/价格） |
| **DApp 代码位置** | `GoldMarketRepository.getAllGamesInfo()` — 回退路径 |

#### 4.3.5 getMyParticipatedGames (读取用户参与的博弈池)

| 项目 | 内容 |
|------|------|
| **方法签名** | `getMyParticipatedGames(address user)` |
| **参数** | `user`: 用户地址 |
| **返回** | `ParticipatedGameDTO[]` — 包含完整信息的结构体数组 |
| **DApp 调用场景** | 个人持仓页加载 |
| **DApp 代码位置** | `GoldMarketRepository.getMyParticipatedGames()` — 回退路径 |

### 4.4 写入交易（eth_sendTransaction）

**端点**: `POST https://dash.broker-chain.com:440/eth_sendTransaction`

**请求格式**:
```json
{
    "PublicKey": "0x04...",
    "RandomStr": "uuid-v4",
    "To": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
    "data": "0x... (ABI encoded)",
    "value": "0x0",
    "Gas": "0x7a1200",
    "Sign1": "0x...",
    "Sign2": "0x..."
}
```

#### 4.4.1 createGame (创建博弈池)

| 项目 | 内容 |
|------|------|
| **方法签名** | `createGame(string ipfsCID, uint256 duration)` |
| **参数** | `ipfsCID`: IPFS 元数据 CID, `duration`: 持续时间 |
| **value** | 初始流动性（BKC wei） |
| **DApp 调用场景** | 用户创建新的博弈池 |
| **前置操作** | 1. 上传图片到 IPFS  2. 上传元数据 JSON 到 IPFS |
| **后置操作** | 同步元数据到后端 DB |

#### 4.4.2 buyShares (买入份额)

| 项目 | 内容 |
|------|------|
| **方法签名** | `buyShares(uint256 gameId, uint8 optionId)` |
| **参数** | `gameId`: 博弈池 ID, `optionId`: 0=YES, 1=NO |
| **value** | 购买金额（BKC wei） |
| **DApp 调用场景** | 用户在详情页下注 |
| **后置操作** | 同步交易记录 + 添加历史价格点到后端 DB |

#### 4.4.3 sellShares (卖出份额)

| 项目 | 内容 |
|------|------|
| **方法签名** | `sellShares(uint256 gameId, uint8 optionId, uint256 shareAmount)` |
| **参数** | `gameId`, `optionId`, `shareAmount`: 卖出份额数量（wei） |
| **value** | 0 |
| **DApp 调用场景** | 用户在详情页卖出 |
| **后置操作** | 同步交易记录 + 添加历史价格点到后端 DB |

#### 4.4.4 claimReward (领取奖励)

| 项目 | 内容 |
|------|------|
| **方法签名** | `claimReward(uint256 gameId, uint8 optionId)` |
| **参数** | `gameId`, `optionId`: 胜出选项 |
| **value** | 0 |
| **DApp 调用场景** | 博弈池结算后用户领取奖励 |
| **后置操作** | 同步交易记录到后端 DB |

#### 4.4.5 resolveGame (管理员开奖)

| 项目 | 内容 |
|------|------|
| **方法签名** | `resolveGame(uint256 gameId, uint8 winningOption)` |
| **参数** | `gameId`, `winningOption`: 胜出选项 (0=YES, 1=NO) |
| **value** | 0 |
| **DApp 调用场景** | 管理员手动触发结算 |
| **后置操作** | 同步交易记录到后端 DB |

---

## 5. Go 后端数据库 API — 完整接口规范

### 5.1 通信协议

| 项目 | 规范 |
|------|------|
| **协议** | HTTP/1.1 over TLS (HTTPS) |
| **数据格式** | JSON (application/json; charset=UTF-8) |
| **字符编码** | UTF-8 |
| **Base URL** | `https://dash.broker-chain.com:440` |
| **API 前缀** | `/api/v1/gold` |
| **超时设置** | 连接 8s，读取 10s（DApp 端） |
| **认证方式** | 无强制认证（读操作公开；写操作通过 ECDSA 签名验证用户身份，Go 端可选校验 `Sign1`/`Sign2`） |
| **版本策略** | URL 路径版本 `/api/v1/...` |
| **限流** | 建议单 IP 100 req/min（Go 端实现） |

### 5.2 通用响应格式

#### 成功响应（HTTP 200）
所有接口在 HTTP 层面返回 200 表示成功，业务数据直接在 body 中：

```json
{
    "games": [ ... ],
    "states": [ ... ],
    "success": true,
    "game_id": 5
}
```

#### 错误响应（HTTP 4xx / 5xx）
```json
{
    "error": {
        "code": "GAME_NOT_FOUND",
        "message": "博弈池 #999 不存在",
        "details": "no row in gold_games where game_id = 999"
    }
}
```

**错误码枚举**:
| HTTP Status | error.code | 说明 |
|-------------|------------|------|
| 400 | `BAD_REQUEST` | 请求参数格式错误 |
| 404 | `GAME_NOT_FOUND` | 指定 gameId 不存在 |
| 404 | `CHAIN_STATE_NOT_FOUND` | 该游戏的链上状态缓存未建立 |
| 409 | `DUPLICATE_GAME` | game_id 已存在（sync 时） |
| 500 | `DATABASE_ERROR` | 数据库查询异常 |
| 502 | `CHAIN_RPC_ERROR` | 后端无法连接链上 RPC |
| 503 | `SERVICE_UNAVAILABLE` | 后端服务暂不可用 |

---

### 5.3 游戏元数据 API

#### 5.3.1 获取所有游戏元数据 `[P0]`

DApp 调用时机：博弈池列表页加载、个人持仓页加载（优先路径，每个页面打开时调用一次）

```
GET /api/v1/gold/games
```

**DApp 侧调用代码**:
```java
// BackendApiClient.fetchAllGameMetadata()
String body = doGet("/games");
// → JSON → List<GameMetaDTO>
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/games" \
     -H "Accept: application/json"
```

**Go 后端 Handler**:
```go
// GET /api/v1/gold/games
func HandleGetAllGames(w http.ResponseWriter, r *http.Request) {
    // SELECT * FROM gold_games ORDER BY game_id DESC
    games, err := db.QueryAllGames()
    // → JSON: {"games": [...]}
}
```

**响应 JSON**:
```json
{
    "games": [
        {
            "game_id": 1,
            "contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
            "ipfs_cid": "QmXxx...xxx",
            "desc": "2026-07-01 黄金价格 上涨",
            "condition": "黄金价格在 2026-07-01 相对基准 上涨 (Price Up)",
            "avatar_url": "QmAvatar123...",
            "detailed_info": "纽约金 COMEX 期货合约价格为准...",
            "option_yes": "达成 (YES)",
            "option_no": "未达成 (NO)",
            "creator_address": "0xAbc...",
            "created_at": "2026-06-20T10:30:00Z"
        }
    ]
}
```

**Java ↔ Go 字段对照**:
| Java (BackendApiClient.GameMetaDTO) | JSON 字段 | Go struct | PostgreSQL 列 | 类型 |
|-------------------------------------|-----------|-----------|---------------|------|
| `gameId` | `game_id` | `GameID int` | `game_id` | integer |
| `contractAddress` | `contract_address` | `ContractAddress string` | `contract_address` | varchar(42) |
| `ipfsCid` | `ipfs_cid` | `IpfsCid string` | `ipfs_cid` | varchar(64) |
| `desc` | `desc` | `Desc string` | `desc` | text |
| `condition` | `condition` | `Condition string` | `condition` | text |
| `avatarUrl` | `avatar_url` | `AvatarUrl string` | `avatar_url` | varchar(128) |
| `detailedInfo` | `detailed_info` | `DetailedInfo string` | `detailed_info` | text |
| `optionYES` | `option_yes` | `OptionYes string` | `option_yes` | varchar(32) |
| `optionNO` | `option_no` | `OptionNo string` | `option_no` | varchar(32) |
| `creatorAddress` | `creator_address` | `CreatorAddress string` | `creator_address` | varchar(42) |
| `createdAt` | `created_at` | `CreatedAt string` | `created_at` | timestamp |

---

#### 5.3.2 获取单个游戏元数据 `[P0]`

DApp 调用时机：博弈池详情页 `getGameInfo()` 后端优先路径

```
GET /api/v1/gold/games/{gameId}
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/games/1" \
     -H "Accept: application/json"
```

**Go 后端 Handler**:
```go
// GET /api/v1/gold/games/{gameId}
func HandleGetGame(w http.ResponseWriter, r *http.Request) {
    gameId := chi.URLParam(r, "gameId")  // 从路径提取
    // SELECT * FROM gold_games WHERE game_id = $1
    game, err := db.QueryGame(gameId)
    if err == sql.ErrNoRows {
        respondError(w, 404, "GAME_NOT_FOUND", "博弈池不存在")
        return
    }
    // → JSON: GameMetaDTO
}
```

**响应**: 单个 `GameMetaDTO` 对象，格式同 5.3.1 中的单个元素。

**错误响应** (404):
```json
{
    "error": {
        "code": "GAME_NOT_FOUND",
        "message": "博弈池 #999 不存在"
    }
}
```

---

#### 5.3.3 同步游戏元数据 `[P0]`

DApp 调用时机：创建博弈池（IPFS 上传 + 链上交易确认后），异步调用，失败不阻塞主流程

```
POST /api/v1/gold/games/sync
Content-Type: application/json; charset=UTF-8
```

**DApp 侧调用代码**:
```java
// BackendApiClient.syncGameMetadata(req)
GameMetaSyncReq req = new GameMetaSyncReq();
req.gameId = 0;         // 0 = 新游戏，后端从链上 event 解析实际 ID
req.ipfsCid = metadataCid;
req.desc = desc;
// ...
String body = doPost("/games/sync", gson.toJson(req));
// → JSON: {"success": true, "game_id": 5}
```

**cURL 示例**:
```bash
curl -X POST "https://dash.broker-chain.com:440/api/v1/gold/games/sync" \
     -H "Content-Type: application/json; charset=UTF-8" \
     -H "Accept: application/json" \
     -d '{
         "game_id": 0,
         "contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
         "ipfs_cid": "QmXxx...xxx",
         "desc": "2026-07-01 黄金价格 上涨",
         "condition": "黄金价格在 2026-07-01 相对基准 上涨",
         "avatar_url": "QmAvatar123...",
         "detailed_info": "...",
         "option_yes": "达成 (YES)",
         "option_no": "未达成 (NO)",
         "creator_address": "0xAbc...",
         "duration_sec": 864000,
         "initial_liquidity_wei": "100000000000000000000"
     }'
```

**请求体 (GameMetaSyncReq)**:

| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `game_id` | int | 是 | 0=新游戏（需后端监听链上 event 获取实际 ID），>0=更新已有游戏 |
| `contract_address` | string | 是 | 合约地址 |
| `ipfs_cid` | string | 是 | IPFS 元数据 CID |
| `desc` | string | 是 | 标题 |
| `condition` | string | 是 | 结算条件 |
| `avatar_url` | string | 否 | 图片 IPFS CID（空串=无图） |
| `detailed_info` | string | 否 | 详细说明 |
| `option_yes` | string | 是 | YES 选项名 |
| `option_no` | string | 是 | NO 选项名 |
| `creator_address` | string | 是 | 创建者地址 |
| `duration_sec` | int64 | 是 | 持续时间（秒） |
| `initial_liquidity_wei` | string | 是 | 初始流动性（十进制字符串，wei 单位） |

**Go 后端 Handler**:
```go
// POST /api/v1/gold/games/sync
func HandleSyncGame(w http.ResponseWriter, r *http.Request) {
    var req GameMetaSyncReq
    json.NewDecoder(r.Body).Decode(&req)

    if req.GameID == 0 {
        // 新游戏：监听链上 GameCreated event 获取实际 gameId
        // 或从 tx receipt logs 中解析
        actualId, err := parseGameCreatedEvent(req.ContractAddress, req.TxHash)
        req.GameID = actualId
    }
    // UPSERT INTO gold_games (...) VALUES (...) ON CONFLICT (game_id) DO UPDATE
    err := db.UpsertGame(req)
    respondJSON(w, 200, map[string]interface{}{
        "success": true,
        "game_id": req.GameID,
    })
}
```

**Go struct 定义**:
```go
type GameMetaSyncReq struct {
    GameID             int    `json:"game_id"`
    ContractAddress    string `json:"contract_address"`
    IpfsCid            string `json:"ipfs_cid"`
    Desc               string `json:"desc"`
    Condition          string `json:"condition"`
    AvatarUrl          string `json:"avatar_url"`
    DetailedInfo       string `json:"detailed_info"`
    OptionYes          string `json:"option_yes"`
    OptionNo           string `json:"option_no"`
    CreatorAddress     string `json:"creator_address"`
    DurationSec        int64  `json:"duration_sec"`
    InitialLiquidityWei string `json:"initial_liquidity_wei"`
}
```

**响应** (200):
```json
{
    "success": true,
    "game_id": 5
}
```

**后端处理逻辑**:
1. 若 `game_id == 0`：从链上 `GameCreated` event 解析出实际的 `game_id`
2. 执行 `INSERT ... ON CONFLICT (game_id) DO UPDATE` (upsert)
3. 返回 `success` 和实际 `game_id`

---

### 5.4 链上状态缓存 API

#### 5.4.1 获取单个游戏链上状态 `[P0]`

DApp 调用时机：博弈池详情页加载（与 `GET /games/{id}` 并发调用）

```
GET /api/v1/gold/games/{gameId}/chain-state?user_address=0x...
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/games/1/chain-state?user_address=0x1234..." \
     -H "Accept: application/json"
```

**查询参数**:
| 参数 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `user_address` | string | 是 | 用户钱包地址（0x 开头，42 字符），用于查询该用户的持仓 |

**Go 后端 Handler**:
```go
// GET /api/v1/gold/games/{gameId}/chain-state?user_address=0x...
func HandleGetChainState(w http.ResponseWriter, r *http.Request) {
    gameId := chi.URLParam(r, "gameId")
    userAddr := r.URL.Query().Get("user_address")
    // 1. SELECT * FROM gold_chain_states WHERE game_id = $1
    // 2. SELECT my_shares_yes, my_shares_no FROM gold_user_positions
    //    WHERE game_id = $1 AND user_address = $2
    // → 合并 → JSON: ChainStateDTO
}
```

**响应 JSON**:
```json
{
    "game_id": 1,
    "total_pool": "500000000000000000000",
    "is_resolved": false,
    "is_refunded": false,
    "winning_option": 0,
    "deadline_sec": 1719878400,
    "reserve_yes": "300000000000000000000",
    "reserve_no": "200000000000000000000",
    "my_shares_yes": "150000000000000000000",
    "my_shares_no": "0",
    "updated_at": "2026-06-26T12:00:00Z"
}
```

**Java ↔ Go 字段对照 (ChainStateDTO)**:
| Java 字段 | JSON 字段 | Go struct 字段 | 来源表 | 类型 |
|-----------|-----------|----------------|--------|------|
| `gameId` | `game_id` | `GameID int` | `gold_chain_states` | integer |
| `totalPool` | `total_pool` | `TotalPool string` | `gold_chain_states` | decimal string |
| `isResolved` | `is_resolved` | `IsResolved bool` | `gold_chain_states` | boolean |
| `isRefunded` | `is_refunded` | `IsRefunded bool` | `gold_chain_states` | boolean |
| `winningOption` | `winning_option` | `WinningOption int` | `gold_chain_states` | smallint |
| `deadlineSec` | `deadline_sec` | `DeadlineSec int64` | `gold_chain_states` | bigint |
| `reserveYES` | `reserve_yes` | `ReserveYes string` | `gold_chain_states` | decimal string |
| `reserveNO` | `reserve_no` | `ReserveNo string` | `gold_chain_states` | decimal string |
| `mySharesYES` | `my_shares_yes` | `MySharesYes string` | `gold_user_positions` | decimal string |
| `mySharesNO` | `my_shares_no` | `MySharesNo string` | `gold_user_positions` | decimal string |
| `updatedAt` | `updated_at` | `UpdatedAt string` | `gold_chain_states` | timestamp |

> **重要**: 大整数（totalPool, reserves, shares）统一使用**十进制字符串**传输，避免 JSON number 精度丢失（JavaScript/Java 的 number 类型无法安全表示 >2^53 的整数）。

---

#### 5.4.2 批量获取所有游戏链上状态 `[P0]`

DApp 调用时机：博弈池列表页加载（与 `GET /games` 并发调用），个人持仓页

```
GET /api/v1/gold/games/chain-states?user_address=0x...
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/games/chain-states?user_address=0x1234..." \
     -H "Accept: application/json"
```

**Go 后端 Handler**:
```go
// GET /api/v1/gold/games/chain-states?user_address=0x...
func HandleGetAllChainStates(w http.ResponseWriter, r *http.Request) {
    userAddr := r.URL.Query().Get("user_address")
    // 1. SELECT * FROM gold_chain_states ORDER BY game_id
    // 2. SELECT * FROM gold_user_positions WHERE user_address = $1
    // 3. 按 game_id 合并 → JSON: {"states": [...]}
}
```

**响应 JSON**:
```json
{
    "states": [
        {
            "game_id": 1,
            "total_pool": "500000000000000000000",
            "is_resolved": false,
            "is_refunded": false,
            "winning_option": 0,
            "deadline_sec": 1719878400,
            "reserve_yes": "300000000000000000000",
            "reserve_no": "200000000000000000000",
            "my_shares_yes": "150000000000000000000",
            "my_shares_no": "0",
            "updated_at": "2026-06-26T12:00:00Z"
        }
    ]
}
```

**DApp 侧并发调用模式**:
```java
// GoldMarketRepository.getAllGamesInfo() — 优先路径
// 两个请求并发发出，Go 端可并行查询
List<GameMetaDTO> metas = BackendApiClient.fetchAllGameMetadata();    // GET /games
List<ChainStateDTO> states = BackendApiClient.fetchAllChainStates(addr); // GET /games/chain-states
// 按 gameId 合并 → List<GameModel>
```

---

#### 5.4.3 同步链上状态 `[P1]`

DApp 调用时机：交易确认后（当前 DApp 版本通过 `POST /trades/sync` 统一同步，此接口预留用于后台定时刷新）

```
POST /api/v1/gold/games/{gameId}/chain-state/sync
```

**Go 端建议**：后端应实现**定时任务**（cron），每 30 秒从链上 `eth_call` 拉取最新状态写入 `gold_chain_states` 和 `gold_user_positions` 表。这样 DApp 读取时始终命中缓存。

---

### 5.5 历史价格 API

#### 5.5.1 获取历史价格数据 `[P1]`

DApp 调用时机：博弈池详情页折线图数据加载

```
GET /api/v1/gold/games/{gameId}/history
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/games/1/history" \
     -H "Accept: application/json"
```

**Go 后端 Handler**:
```go
// GET /api/v1/gold/games/{gameId}/history
func HandleGetHistory(w http.ResponseWriter, r *http.Request) {
    gameId := chi.URLParam(r, "gameId")
    // SELECT timestamp_sec, yes_price, no_price, total_pool
    // FROM gold_price_history
    // WHERE game_id = $1
    // ORDER BY timestamp_sec ASC
    rows, _ := db.QueryHistory(gameId)
    // → JSON: {"history": [...]}
}
```

**响应 JSON**:
```json
{
    "history": [
        {
            "game_id": 1,
            "timestamp_sec": 1719878400,
            "yes_price": 65.5,
            "no_price": 34.5,
            "total_pool": "500000000000000000000"
        },
        {
            "game_id": 1,
            "timestamp_sec": 1719964800,
            "yes_price": 68.2,
            "no_price": 31.8,
            "total_pool": "550000000000000000000"
        }
    ]
}
```

**字段类型**:
| 字段 | 类型 | 说明 |
|------|------|------|
| `game_id` | int | 博弈池 ID |
| `timestamp_sec` | int64 | Unix 时间戳（秒） |
| `yes_price` | float32 | YES 选项百分比 (0-100) |
| `no_price` | float32 | NO 选项百分比 (0-100)，恒等于 `100 - yes_price` |
| `total_pool` | string | 当时的总池子（wei，十进制字符串） |

**DApp 侧数据流**:
```
BackendApiClient.fetchHistory(gameId)
    → List<HistoryPointDTO>
    → GoldMarketRepository.buildModelFromBackend() 注入 model.history
    → GoldMarketDetailActivity.setupHistoryChart() 渲染折线图
```

---

#### 5.5.2 添加历史价格点 `[P1]`

DApp 调用时机：每次 buy/sell 交易确认后（异步，非阻塞）

```
POST /api/v1/gold/games/{gameId}/history
Content-Type: application/json; charset=UTF-8
```

**cURL 示例**:
```bash
curl -X POST "https://dash.broker-chain.com:440/api/v1/gold/games/1/history" \
     -H "Content-Type: application/json; charset=UTF-8" \
     -d '{
         "game_id": 1,
         "timestamp_sec": 1719878400,
         "yes_price": 68.2,
         "no_price": 31.8,
         "total_pool": "550000000000000000000"
     }'
```

**请求体 (HistoryPointDTO)**:
| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `game_id` | int | 是 | 博弈池 ID |
| `timestamp_sec` | int64 | 是 | 当前 Unix 时间戳（秒），DApp 使用 `System.currentTimeMillis() / 1000` |
| `yes_price` | float | 是 | 当前 YES 价格百分比，由 `reserveNO/(reserveNO+reserveYES)*100` 计算；原因是当前合约中买入 YES 时，注入的是 NO 侧储备，YES 的隐含价格取对侧储备占比 |
| `no_price` | float | 是 | 当前 NO 价格百分比，`100 - yes_price` |
| `total_pool` | string | 否 | 当前总池子（wei，十进制字符串） |

**Go 后端 Handler**:
```go
// POST /api/v1/gold/games/{gameId}/history
func HandleAddHistoryPoint(w http.ResponseWriter, r *http.Request) {
    gameId := chi.URLParam(r, "gameId")
    var point HistoryPointDTO
    json.NewDecoder(r.Body).Decode(&point)
    point.GameID = gameId  // 以路径参数为准
    // INSERT INTO gold_price_history (game_id, timestamp_sec, yes_price, no_price, total_pool)
    // VALUES ($1, $2, $3, $4, $5)
    err := db.InsertHistoryPoint(point)
    respondJSON(w, 200, map[string]bool{"success": err == nil})
}
```

**响应** (200):
```json
{
    "success": true
}
```

**去重建议**：Go 端可在 `(game_id, timestamp_sec)` 上建唯一索引，使用 `INSERT ... ON CONFLICT DO NOTHING` 避免同一秒内重复写入。

---

### 5.6 交易同步 API

#### 5.6.1 同步交易记录 `[P0]`

DApp 调用时机：每次链上交易确认后（buy/sell/claim/resolve），异步调用，失败不阻塞 UI

```
POST /api/v1/gold/trades/sync
Content-Type: application/json; charset=UTF-8
```

**cURL 示例**:
```bash
curl -X POST "https://dash.broker-chain.com:440/api/v1/gold/trades/sync" \
     -H "Content-Type: application/json; charset=UTF-8" \
     -d '{
         "game_id": 1,
         "contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
         "user_address": "0xAbc...",
         "trade_type": "BUY",
         "option_id": 0,
         "amount_wei": "100000000000000000000",
         "tx_hash": "0xDef...",
         "is_success": true,
         "total_pool_after": "600000000000000000000",
         "reserve_yes_after": "400000000000000000000",
         "reserve_no_after": "200000000000000000000",
         "my_shares_yes_after": "250000000000000000000",
         "my_shares_no_after": "0"
     }'
```

**请求体 (TradeSyncReq)**:
| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `game_id` | int | 是 | 博弈池 ID |
| `contract_address` | string | 是 | 合约地址 |
| `user_address` | string | 是 | 用户钱包地址 |
| `trade_type` | string | 是 | `BUY` / `SELL` / `CLAIM` / `RESOLVE` |
| `option_id` | int | 是 | 0=YES, 1=NO |
| `amount_wei` | string | 是 | 交易金额（wei，十进制字符串） |
| `tx_hash` | string | 是 | 链上交易哈希 |
| `is_success` | bool | 是 | 交易是否成功 |
| `total_pool_after` | string | 否 | 交易后总池子 |
| `reserve_yes_after` | string | 否 | 交易后 YES 储备金 |
| `reserve_no_after` | string | 否 | 交易后 NO 储备金 |
| `my_shares_yes_after` | string | 否 | 交易后用户 YES 份额 |
| `my_shares_no_after` | string | 否 | 交易后用户 NO 份额 |

**trade_type 枚举**:
| 值 | DApp 触发场景 | 含义 |
|----|-------------|------|
| `BUY` | 用户点击「买入 YES/NO」 | 买入份额 |
| `SELL` | 用户卖出持仓份额 | 卖出份额 |
| `CLAIM` | 博弈池结算后领取奖励 | 领取奖励 |
| `RESOLVE` | 管理员点击「开奖」 | 管理员结算 |

**Go 后端 Handler（核心逻辑）**:
```go
// POST /api/v1/gold/trades/sync
func HandleSyncTrade(w http.ResponseWriter, r *http.Request) {
    var req TradeSyncReq
    json.NewDecoder(r.Body).Decode(&req)

    // 1. 写入交易记录表
    // INSERT INTO gold_trades (game_id, user_address, trade_type, option_id,
    //   amount_wei, tx_hash, is_success) VALUES (...)
    db.InsertTrade(req)

    // 2. 更新链上状态缓存（如果提供了 after 字段）
    if req.TotalPoolAfter != "" {
        // UPDATE gold_chain_states SET total_pool = $1, reserve_yes = $2,
        //   reserve_no = $3, is_resolved = $4, ... WHERE game_id = $5
        db.UpdateChainState(req)
    }

    // 3. 更新用户持仓（upsert）
    // INSERT INTO gold_user_positions (user_address, game_id, my_shares_yes, my_shares_no)
    // VALUES (...) ON CONFLICT (user_address, game_id) DO UPDATE ...
    db.UpsertUserPosition(req)

    // 4. 同时写入历史价格点（从 after 数据计算）
    // INSERT INTO gold_price_history (game_id, timestamp_sec, yes_price, no_price, total_pool) ...
    go db.InsertHistoryPointFromTrade(req)  // 异步

    respondJSON(w, 200, map[string]bool{"success": true})
}
```

**Go struct 定义**:
```go
type TradeSyncReq struct {
    GameID           int    `json:"game_id"`
    ContractAddress  string `json:"contract_address"`
    UserAddress      string `json:"user_address"`
    TradeType        string `json:"trade_type"`
    OptionID         int    `json:"option_id"`
    AmountWei        string `json:"amount_wei"`
    TxHash           string `json:"tx_hash"`
    IsSuccess        bool   `json:"is_success"`
    TotalPoolAfter   string `json:"total_pool_after"`
    ReserveYESAfter  string `json:"reserve_yes_after"`
    ReserveNOAfter   string `json:"reserve_no_after"`
    MySharesYESAfter string `json:"my_shares_yes_after"`
    MySharesNOAfter  string `json:"my_shares_no_after"`
}
```

**响应** (200):
```json
{
    "success": true
}
```

---

### 5.7 AI 托管 API

#### 5.7.1 查询 AI 托管状态 `[P1]`

DApp 调用时机：博弈池详情页 `loadGameInfo()` 中，获取链上数据后查询

```
GET /api/v1/gold/ai-managed?game_id=1&user_address=0x...&contract_address=0x...
```

**cURL 示例**:
```bash
curl -X GET "https://dash.broker-chain.com:440/api/v1/gold/ai-managed?game_id=1&user_address=0x1234...&contract_address=0xad4F..." \
     -H "Accept: application/json"
```

**查询参数**:
| 参数 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `game_id` | int | 是 | 博弈池 ID |
| `user_address` | string | 是 | 用户钱包地址 |
| `contract_address` | string | 是 | 合约地址 |

**Go 后端 Handler**:
```go
// GET /api/v1/gold/ai-managed?game_id=X&user_address=Y&contract_address=Z
func HandleGetAiManaged(w http.ResponseWriter, r *http.Request) {
    gameId := r.URL.Query().Get("game_id")
    userAddr := r.URL.Query().Get("user_address")
    contractAddr := r.URL.Query().Get("contract_address")
    // SELECT enabled FROM gold_ai_managed
    // WHERE game_id = $1 AND user_address = $2 AND contract_address = $3
    row := db.QueryRow(...)
    var enabled bool
    row.Scan(&enabled)
    // → JSON: {"enabled": true/false}
}
```

**响应** (200):
```json
{
    "enabled": true
}
```

> 未找到记录时返回 `{"enabled": false}`，不报错。

---

#### 5.7.2 设置 AI 托管状态 `[P1]`

DApp 调用时机：用户在详情页切换 AI 托管开关

```
POST /api/v1/gold/ai-managed
Content-Type: application/json; charset=UTF-8
```

**cURL 示例**:
```bash
curl -X POST "https://dash.broker-chain.com:440/api/v1/gold/ai-managed" \
     -H "Content-Type: application/json; charset=UTF-8" \
     -d '{
         "game_id": 1,
         "user_address": "0xAbc...",
         "enabled": true,
         "contract_address": "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
         "private_key": "0x..."
     }'
```

**请求体**:
| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `game_id` | int | 是 | 博弈池 ID |
| `user_address` | string | 是 | 用户钱包地址 |
| `enabled` | bool | 是 | 是否启用 AI 托管 |
| `contract_address` | string | 是 | 合约地址 |
| `private_key` | string | 是 | 用户私钥（供后端 AI 代理下单使用） |

**Go 后端 Handler**:
```go
// POST /api/v1/gold/ai-managed
func HandleSetAiManaged(w http.ResponseWriter, r *http.Request) {
    var req AiManagedReq
    json.NewDecoder(r.Body).Decode(&req)
    // UPSERT INTO gold_ai_managed (user_address, game_id, contract_address, enabled, private_key_encrypted)
    // VALUES (...) ON CONFLICT (user_address, game_id) DO UPDATE SET enabled=$4, private_key_encrypted=$5
    err := db.UpsertAiManaged(req)
    respondJSON(w, 200, map[string]bool{"success": err == nil})
}
```

**Go struct 定义**:
```go
type AiManagedReq struct {
    GameID          int    `json:"game_id"`
    UserAddress     string `json:"user_address"`
    Enabled         bool   `json:"enabled"`
    ContractAddress string `json:"contract_address"`
    PrivateKey      string `json:"private_key"`
}
```

**响应** (200):
```json
{
    "success": true
}
```

> **安全注意**: `private_key` 仅在 HTTPS 下传输。生产环境建议：1) 使用 ECDSA 签名代替直接传输私钥；2) Go 端使用 AES-256-GCM 加密存储私钥。

---

### 5.8 Go 后端路由汇总

```go
// Go 后端推荐使用 chi router 或 gin
func RegisterGoldRoutes(r chi.Router) {
    // 游戏元数据
    r.Get("/api/v1/gold/games", HandleGetAllGames)              // [P0] 所有游戏元数据
    r.Get("/api/v1/gold/games/{gameId}", HandleGetGame)         // [P0] 单个游戏元数据
    r.Post("/api/v1/gold/games/sync", HandleSyncGame)           // [P0] 同步游戏元数据

    // 链上状态缓存
    r.Get("/api/v1/gold/games/{gameId}/chain-state", HandleGetChainState)       // [P0] 单个链上状态
    r.Get("/api/v1/gold/games/chain-states", HandleGetAllChainStates)            // [P0] 批量链上状态
    r.Post("/api/v1/gold/games/{gameId}/chain-state/sync", HandleSyncChainState) // [P1] 同步链上状态

    // 历史价格
    r.Get("/api/v1/gold/games/{gameId}/history", HandleGetHistory)    // [P1] 获取历史
    r.Post("/api/v1/gold/games/{gameId}/history", HandleAddHistory)   // [P1] 添加历史点

    // 交易同步
    r.Post("/api/v1/gold/trades/sync", HandleSyncTrade)              // [P0] 同步交易

    // AI 托管
    r.Get("/api/v1/gold/ai-managed", HandleGetAiManaged)             // [P1] 查询托管状态
    r.Post("/api/v1/gold/ai-managed", HandleSetAiManaged)            // [P1] 设置托管状态
}
```

### 5.9 Go struct 定义汇总

```go
package gold

// ── 游戏元数据 ──
type GameMetaDTO struct {
    GameID          int    `json:"game_id"`
    ContractAddress string `json:"contract_address"`
    IpfsCid         string `json:"ipfs_cid"`
    Desc            string `json:"desc"`
    Condition       string `json:"condition"`
    AvatarUrl       string `json:"avatar_url"`
    DetailedInfo    string `json:"detailed_info"`
    OptionYes       string `json:"option_yes"`
    OptionNo        string `json:"option_no"`
    CreatorAddress  string `json:"creator_address"`
    CreatedAt       string `json:"created_at"`
}

// ── 游戏元数据同步请求 ──
type GameMetaSyncReq struct {
    GameID              int    `json:"game_id"`
    ContractAddress     string `json:"contract_address"`
    IpfsCid             string `json:"ipfs_cid"`
    Desc                string `json:"desc"`
    Condition           string `json:"condition"`
    AvatarUrl           string `json:"avatar_url"`
    DetailedInfo        string `json:"detailed_info"`
    OptionYes           string `json:"option_yes"`
    OptionNo            string `json:"option_no"`
    CreatorAddress      string `json:"creator_address"`
    DurationSec         int64  `json:"duration_sec"`
    InitialLiquidityWei string `json:"initial_liquidity_wei"`
}

// ── 链上状态缓存 ──
type ChainStateDTO struct {
    GameID       int    `json:"game_id"`
    TotalPool    string `json:"total_pool"`
    IsResolved   bool   `json:"is_resolved"`
    IsRefunded   bool   `json:"is_refunded"`
    WinningOption int   `json:"winning_option"`
    DeadlineSec  int64  `json:"deadline_sec"`
    ReserveYes   string `json:"reserve_yes"`
    ReserveNo    string `json:"reserve_no"`
    MySharesYes  string `json:"my_shares_yes"`
    MySharesNo   string `json:"my_shares_no"`
    UpdatedAt    string `json:"updated_at"`
}

// ── 历史价格点 ──
type HistoryPointDTO struct {
    GameID      int     `json:"game_id"`
    TimestampSec int64  `json:"timestamp_sec"`
    YesPrice    float32 `json:"yes_price"`
    NoPrice     float32 `json:"no_price"`
    TotalPool   string  `json:"total_pool"`
}

// ── 交易同步 ──
type TradeSyncReq struct {
    GameID           int    `json:"game_id"`
    ContractAddress  string `json:"contract_address"`
    UserAddress      string `json:"user_address"`
    TradeType        string `json:"trade_type"`  // BUY | SELL | CLAIM | RESOLVE
    OptionID         int    `json:"option_id"`
    AmountWei        string `json:"amount_wei"`
    TxHash           string `json:"tx_hash"`
    IsSuccess        bool   `json:"is_success"`
    TotalPoolAfter   string `json:"total_pool_after"`
    ReserveYESAfter  string `json:"reserve_yes_after"`
    ReserveNOAfter   string `json:"reserve_no_after"`
    MySharesYESAfter string `json:"my_shares_yes_after"`
    MySharesNOAfter  string `json:"my_shares_no_after"`
}

// ── AI 托管 ──
type AiManagedReq struct {
    GameID          int    `json:"game_id"`
    UserAddress     string `json:"user_address"`
    Enabled         bool   `json:"enabled"`
    ContractAddress string `json:"contract_address"`
    PrivateKey      string `json:"private_key"`
}

// ── 通用错误 ──
type ErrorResponse struct {
    Error ErrorDetail `json:"error"`
}
type ErrorDetail struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details string `json:"details,omitempty"`
}
```

### 5.10 大整数传输规范（关键！）

由于区块链上的金额（totalPool、reserves、shares）使用 uint256，远超 JSON number 的安全范围（2^53），**所有大整数字段统一使用十进制字符串传输**：

```json
{
    "total_pool": "500000000000000000000",    ← ✅ 正确：字符串
    "total_pool": 500000000000000000000       ← ❌ 错误：JSON number 精度丢失
}
```

| 字段 | Java 类型 | JSON 类型 | Go 类型 | PostgreSQL 类型 |
|------|----------|-----------|---------|-----------------|
| `total_pool` | String | string | string | DECIMAL(78,0) → string |
| `reserve_yes` | String | string | string | DECIMAL(78,0) → string |
| `reserve_no` | String | string | string | DECIMAL(78,0) → string |
| `my_shares_yes` | String | string | string | DECIMAL(78,0) → string |
| `my_shares_no` | String | string | string | DECIMAL(78,0) → string |
| `amount_wei` | String | string | string | DECIMAL(78,0) → string |
| `initial_liquidity_wei` | String | string | string | DECIMAL(78,0) → string |

**Go 端实现建议**:
```go
import "math/big"

// 数据库读取时自动转为十进制字符串
func scanBigInt(src interface{}) string {
    if src == nil { return "0" }
    n := new(big.Int)
    n.SetString(string(src.([]byte)), 10)
    return n.String()
}
```

---

### 5.11 DApp 侧调用时序图

```
DApp (Android/Java)                          Go 后端                          PostgreSQL
══════════════════                          ════════                          ══════════

┌─ 页面打开 ──────────────────────────────────────────────────────────────────────┐

  GoldMarketRepository
  .getAllGamesInfo()
      │
      ├── BackendApiClient                ──GET /games──►
      │   .fetchAllGameMetadata()                          ├─ SELECT * FROM gold_games
      │                                                    │  ← [GameMetaDTO, ...]
      │                              ◄──200 OK {games}──  ─┘
      │
      ├── BackendApiClient                ──GET /chain-states──►
      │   .fetchAllChainStates(addr)                        ├─ SELECT * FROM gold_chain_states
      │                                                    ├─ SELECT * FROM gold_user_positions
      │                          ◄──200 OK {states}──      ─┘ WHERE user_address=$1
      │
      └── 合并 metas + states → List<GameModel> → UI 渲染

└──────────────────────────────────────────────────────────────────────────────────┘

┌─ 买入交易 ──────────────────────────────────────────────────────────────────────┐

  GoldMarketRepository
  .buyShares()
      │
      ├── BrokerChainClient               ──eth_sendTransaction──►  区块链
      │   .sendEthTx()                                          确认...
      │                              ◄──txHash────────────────
      │
      └── [异步] BackendApiClient
          ├── .syncTrade(req)             ──POST /trades/sync──►
          │                               ├─ INSERT gold_trades
          │                               ├─ UPDATE gold_chain_states
          │                               ├─ UPSERT gold_user_positions
          │                               └─ INSERT gold_price_history
          │                ◄──200 {success}──
          │
          └── .addHistoryPoint()          ──POST /{id}/history──►
                                          └─ INSERT gold_price_history
                              ◄──200 {success}──
```

### 5.12 Go 后端定时同步任务

Go 后端需实现定时 cron job 以保证缓存数据新鲜度：

```go
// 每 30 秒执行一次
func CronSyncChainState() {
    games, _ := db.QueryAllGames()
    for _, g := range games {
        // 1. eth_call getGameInfo(g.GameID) → 获取链上最新状态
        chainData := brokerChainClient.EthCall("getGameInfo", g.GameID)

        // 2. UPDATE gold_chain_states SET ... WHERE game_id = g.GameID
        db.UpdateChainState(g.GameID, chainData)

        // 3. eth_call getGameExtraData(g.GameID, user) → 更新活跃用户的持仓
        activeUsers := db.QueryActiveUsers(g.GameID)
        for _, user := range activeUsers {
            extra := brokerChainClient.EthCall("getGameExtraData", g.GameID, user)
            db.UpsertUserPosition(g.GameID, user, extra)
        }
    }
}
```

---

## 6. 数据流图

### 6.1 读取博弈池列表（优先路径：后端 DB）

```
用户打开 App
    │
    ▼
GoldMarketViewModel.loadData()
    │
    ▼
GoldMarketRepository.getAllGamesInfo()
    │
    ├── 1. BackendApiClient.fetchAllGameMetadata()     ──► Go 后端 DB
    │       └── GET /api/v1/gold/games
    │       └── 返回: List<GameMetaDTO>（标题、条件、图片等）       ⚡ ~50ms
    │
    ├── 2. BackendApiClient.fetchAllChainStates()      ──► Go 后端 DB
    │       └── GET /api/v1/gold/games/chain-states
    │       └── 返回: List<ChainStateDTO>（储备金、份额等）          ⚡ ~50ms
    │
    └── 3. 合并 DTO → 构建 List<GameModel> → 返回 UI
```

### 6.2 读取博弈池详情（回退路径：链上 + IPFS）

```
后端 DB 不可用时，自动回退：
    │
    ▼
GoldMarketRepository.getGameInfo() [fallback]
    │
    ├── 1. ethCall(getGameInfo) + ethCall(getGameExtraData)   ──► 区块链
    │       └── 返回: ipfsCID, totalPool, reserves, shares...      🐢 ~500ms
    │
    ├── 2. PinataClient.downloadJsonFromIPFS(cid)              ──► IPFS
    │       └── GET /ipfs/{cid}
    │       └── 返回: desc, condition, avatarUrl...                🐢 ~200ms
    │
    └── 3. 构建 GameModel → 返回 UI
```

### 6.3 创建博弈池（完整写入流程）

```
用户点击「部署博弈池」
    │
    ▼
GoldCreatePoolViewModel.createGame()
    │
    ▼
GoldMarketRepository.createGame()
    │
    ├── 步骤 1: 上传图片到 IPFS                           ──► IPFS
    │       └── POST /api/v0/add (multipart)
    │       └── 返回: avatarCid
    │
    ├── 步骤 2: 上传元数据 JSON 到 IPFS                   ──► IPFS
    │       └── POST /api/v0/add (JSON)
    │       └── 返回: metadataCid
    │
    ├── 步骤 3: 发送 createGame 链上交易                  ──► 区块链
    │       └── eth_sendTransaction(createGame, ipfsCID, duration)
    │       └── 等待确认...
    │
    └── 步骤 4: 交易确认后，同步元数据到后端 DB（异步）   ──► Go 后端 DB
            └── POST /api/v1/gold/games/sync
            └── 失败不影响主流程（非关键路径）
```

### 6.4 买入/卖出（交易写入流程）

```
用户点击「买入 YES」 / 「卖出 NO」
    │
    ▼
GoldMarketDetailViewModel.buyShares() / sellShares()
    │
    ▼
GoldMarketRepository.buyShares() / sellShares()
    │
    ├── 步骤 1: 发送链上交易                               ──► 区块链
    │       └── eth_sendTransaction(buyShares/sellShares)
    │       └── 等待确认...
    │
    └── 步骤 2: 交易确认后，同步到后端 DB（异步）          ──► Go 后端 DB
            ├── POST /api/v1/gold/trades/sync     (交易记录)
            └── POST /api/v1/gold/games/{id}/history (价格快照)
            └── 失败不影响主流程（非关键路径）
```

---

## 7. 错误处理与回退策略

### 7.1 读操作回退链

```
优先级 1: 后端 DB
    │
    ├── 成功 ──► 返回数据（毫秒级）
    │
    └── 失败（网络超时/500/未找到）
            │
            ▼
        优先级 2: 链上 eth_call + IPFS
            │
            ├── 成功 ──► 返回数据（较慢但可靠）
            │
            └── 失败 ──► 返回错误给 UI
```

### 7.2 写操作容错

```
步骤 1: IPFS 上传
    └── 失败 ──► 终止流程，返回错误。图片上传失败不阻塞（可选字段）

步骤 2: 链上交易
    └── 失败 ──► 返回错误给 UI

步骤 3: 后端 DB 同步
    └── 失败 ──► 记录日志，不影响主流程（非关键路径）
                  后端可通过定时任务从链上重新同步数据
```

### 7.3 后端 DB 不可用时的行为

| 场景 | 行为 |
|------|------|
| 列表页加载 | 自动回退到链上 + IPFS 直读，用户可能感觉加载变慢 |
| 详情页加载 | 自动回退到链上 + IPFS 直读 |
| 历史价格数据 | 使用本地生成的模拟数据 |
| AI 托管状态 | 回退到原有直连 API |
| 交易后同步 | 静默失败，后端通过定时任务补同步 |

---

## 8. 性能对比

### 8.1 读取操作延迟对比

| 操作 | 后端 DB 路径 | 链上+IPFS 回退路径 | 性能提升 |
|------|-------------|-------------------|----------|
| 加载博弈池列表 (10个) | ~100ms | ~1500-3000ms | **15-30x** |
| 加载博弈池详情 | ~80ms | ~500-1000ms | **6-12x** |
| 加载个人持仓 (5个) | ~80ms | ~800-2000ms | **10-25x** |
| 加载历史价格数据 | ~50ms | ~200ms (IPFS) | **4x** |

### 8.2 写入操作延迟

| 操作 | 主要耗时 | 后端同步额外耗时 |
|------|---------|-----------------|
| 创建博弈池 | IPFS 上传 (~500ms) + 链上确认 (~5-30s) | +~50ms (异步) |
| 买入/卖出 | 链上确认 (~5-30s) | +~50ms (异步) |
| 领取奖励 | 链上确认 (~5-30s) | +~50ms (异步) |

> 后端同步为异步非阻塞操作，不影响用户感知的写入延迟。

---

## 附录 A: DApp 代码文件索引

| 文件 | 职责 |
|------|------|
| `agent/gold/model/data/BackendApiClient.java` | **[新增]** Go 后端 DB API 客户端 |
| `agent/gold/model/data/GoldMarketRepository.java` | **[修改]** 三层数据仓库核心逻辑 |
| `agent/gold/model/data/PinataClient.java` | IPFS 上传/下载客户端 |
| `agent/gold/model/data/BrokerChainClient.java` | 区块链 RPC 客户端（签名+发送） |
| `agent/gold/model/data/AppExecutors.java` | 线程池管理 |
| `agent/gold/viewmodel/GoldMarketViewModel.java` | 博弈池列表 ViewModel |
| `agent/gold/viewmodel/GoldMarketDetailViewModel.java` | 博弈池详情 ViewModel |
| `agent/gold/viewmodel/GoldCreatePoolViewModel.java` | 创建博弈池 ViewModel |
| `agent/gold/viewmodel/GoldMyPositionsViewModel.java` | 个人持仓 ViewModel |

## 附录 B: Go 后端需实现的 API 清单

| 序号 | 方法 | 端点 | 优先级 | 说明 |
|------|------|------|--------|------|
| 1 | GET | `/api/v1/gold/games` | P0 | 从 DB 查询所有游戏元数据 |
| 2 | GET | `/api/v1/gold/games/{id}` | P0 | 从 DB 查询单个游戏元数据 |
| 3 | POST | `/api/v1/gold/games/sync` | P0 | 创建/更新游戏元数据 |
| 4 | GET | `/api/v1/gold/games/{id}/chain-state` | P0 | 查询缓存的链上状态 |
| 5 | GET | `/api/v1/gold/games/chain-states` | P0 | 批量查询所有游戏链上状态 |
| 6 | POST | `/api/v1/gold/games/{id}/chain-state/sync` | P1 | 主动更新链上状态缓存 |
| 7 | GET | `/api/v1/gold/games/{id}/history` | P1 | 查询历史价格数据 |
| 8 | POST | `/api/v1/gold/games/{id}/history` | P1 | 添加历史价格点 |
| 9 | POST | `/api/v1/gold/trades/sync` | P0 | 同步交易记录 |
| 10 | GET | `/api/v1/gold/ai-managed` | P1 | 查询 AI 托管状态 |
| 11 | POST | `/api/v1/gold/ai-managed` | P1 | 设置 AI 托管状态 |

### 后端 DB 表设计建议

```sql
-- 游戏元数据表
CREATE TABLE gold_games (
    game_id INTEGER PRIMARY KEY,
    contract_address VARCHAR(42) NOT NULL,
    ipfs_cid VARCHAR(64) NOT NULL,
    desc TEXT,
    condition TEXT,
    avatar_url VARCHAR(128),
    detailed_info TEXT,
    option_yes VARCHAR(32) DEFAULT 'YES',
    option_no VARCHAR(32) DEFAULT 'NO',
    creator_address VARCHAR(42),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- 链上状态缓存表
CREATE TABLE gold_chain_states (
    game_id INTEGER PRIMARY KEY REFERENCES gold_games(game_id),
    total_pool DECIMAL(78,0),
    is_resolved BOOLEAN DEFAULT FALSE,
    is_refunded BOOLEAN DEFAULT FALSE,
    winning_option SMALLINT DEFAULT 0,
    deadline_sec BIGINT,
    reserve_yes DECIMAL(78,0),
    reserve_no DECIMAL(78,0),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- 用户持仓表（按用户地址 + 游戏 ID 联合）
CREATE TABLE gold_user_positions (
    user_address VARCHAR(42),
    game_id INTEGER REFERENCES gold_games(game_id),
    my_shares_yes DECIMAL(78,0) DEFAULT 0,
    my_shares_no DECIMAL(78,0) DEFAULT 0,
    updated_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (user_address, game_id)
);

-- 历史价格表
CREATE TABLE gold_price_history (
    id SERIAL PRIMARY KEY,
    game_id INTEGER REFERENCES gold_games(game_id),
    timestamp_sec BIGINT NOT NULL,
    yes_price REAL,
    no_price REAL,
    total_pool DECIMAL(78,0)
);
CREATE INDEX idx_history_game_time ON gold_price_history(game_id, timestamp_sec);

-- 交易记录表
CREATE TABLE gold_trades (
    id SERIAL PRIMARY KEY,
    game_id INTEGER REFERENCES gold_games(game_id),
    user_address VARCHAR(42),
    trade_type VARCHAR(10),  -- BUY/SELL/CLAIM/RESOLVE
    option_id SMALLINT,
    amount_wei DECIMAL(78,0),
    tx_hash VARCHAR(66),
    is_success BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW()
);

-- AI 托管配置表
CREATE TABLE gold_ai_managed (
    user_address VARCHAR(42),
    game_id INTEGER REFERENCES gold_games(game_id),
    contract_address VARCHAR(42),
    enabled BOOLEAN DEFAULT FALSE,
    private_key_encrypted TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (user_address, game_id)
);
```
