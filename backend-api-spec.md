# 后端 API 对齐文档 — 黄金票据博弈池

> 本文档详细说明 DApp（Android 端）与 Golang 后端之间的交互方式、API 规范、数据库设计要求以及 IPFS 交互方式。
>
> 版本: v1.0 | 日期: 2026-06-23

---

## 目录

1. [架构总览](#1-架构总览)
2. [数据分层策略](#2-数据分层策略)
3. [RESTful API 规范](#3-restful-api-规范)
4. [数据库表设计建议](#4-数据库表设计建议)
5. [后端数据同步机制](#5-后端数据同步机制)
6. [IPFS 交互规范](#6-ipfs-交互规范)
7. [错误处理与回退](#7-错误处理与回退)
8. [附录：现有接口（兼容保留）](#8-附录现有接口兼容保留)

---

## 1. 架构总览

```
┌──────────────────────────────────────────────────────────────────┐
│                        DApp (Android)                            │
│                                                                  │
│  ┌──────────────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │ GoldMarketRepository │  │ PinataClient  │  │ BackendApi    │  │
│  │ (数据编排层)          │  │ (IPFS 读写)   │  │ Client (HTTP) │  │
│  └──────┬───────────────┘  └──────┬───────┘  └───────┬───────┘  │
│         │                         │                   │          │
└─────────┼─────────────────────────┼───────────────────┼──────────┘
          │                         │                   │
          │ 写操作                   │ 元数据读写          │ 读操作
          │ (买入/卖出/创建/领奖)     │ (JSON/图片)        │ (列表/详情/持仓/历史)
          ▼                         ▼                   ▼
    ┌──────────┐            ┌──────────────┐    ┌──────────────┐
    │ Blockchain│            │  IPFS 节点    │    │ Golang 后端   │
    │ (合约)    │            │ (本地/远程)    │    │ (MySQL/PG)   │
    └──────────┘            └──────────────┘    └──────────────┘
```

### 数据流方向

| 操作类型 | 数据流向 | 说明 |
|---------|---------|------|
| 买入 YES/NO | DApp → **Blockchain** → 通知后端同步 | 写操作走链，保证去中心化 |
| 卖出份额 | DApp → **Blockchain** → 通知后端同步 | 同上 |
| 创建博弈池 | DApp → **IPFS**(元数据) → **Blockchain**(CID+参数) → 通知后端同步 | 先 IPFS 后链 |
| 领取奖励 | DApp → **Blockchain** → 通知后端同步 | 同上 |
| 开奖 | DApp → **Blockchain** → 通知后端同步 | 后台管理操作 |
| 加载博弈列表 | DApp → **后端 API**（快）→ **IPFS**（元数据） | 读操作走后端 |
| 加载博弈详情 | DApp → **后端 API**（快）→ **IPFS**（元数据） | 同上 |
| 加载个人持仓 | DApp → **后端 API**（快）→ **IPFS**（元数据） | 同上 |
| 加载历史折线图 | DApp → **后端 API** | 纯后端数据 |
| AI 托管开关 | DApp → **后端 API** | 后端轮询下单 |

---

## 2. 数据分层策略

### 三层数据模型

```
Layer 1 — Blockchain（链上）
  ├─ 合约地址、合约状态
  ├─ 博弈池核心参数（ipfsCID, totalPool, isResolved, winningOption, deadlineSec, isRefunded）
  ├─ 储备金（virtualReserves）
  ├─ 用户持仓份额（myShares）
  └─ 写操作入口（buyShares, sellShares, createGame, claimReward, resolveGame）

Layer 2 — IPFS（去中心化存储）
  ├─ 博弈池元数据 JSON（desc, condition, avatarUrl, detailedInfo, optionYES, optionNO）
  ├─ 图片资源（头像等二进制文件）
  └─ 特性：不可篡改、去中心化、适合不需要实时刷新的数据

Layer 3 — Backend DB（MySQL/PostgreSQL）
  ├─ 链上数据的同步缓存（加速读取）
  ├─ 历史价格数据（observed_at, yes_percent, no_percent）
  ├─ AI 托管状态
  └─ 特性：快速查询、支持复杂过滤、可扩展
```

### 数据归属决策表

| 数据字段 | 真实来源 | 读路径 | 写路径 | 刷新频率 |
|---------|---------|--------|--------|---------|
| ipfsCID | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| totalPool | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| isResolved | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| winningOption | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| deadlineSec | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| isRefunded | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| virtualReserves | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| myShares | Blockchain | 后端 DB 缓存 | 链上写 | 实时同步 |
| desc | IPFS | IPFS 网关 | IPFS 上传 | 创建时写入 |
| condition | IPFS | IPFS 网关 | IPFS 上传 | 创建时写入 |
| avatarUrl | IPFS | IPFS 网关 | IPFS 上传 | 创建时写入 |
| detailedInfo | IPFS | IPFS 网关 | IPFS 上传 | 创建时写入 |
| optionNames | IPFS | IPFS 网关 | IPFS 上传 | 创建时写入 |
| yesPrice/noPrice | 后端计算 | 后端 DB | 后端写入 | 事件驱动 |
| isManaged | 后端 DB | 后端 API | 后端 API | 用户操作 |

---

## 3. RESTful API 规范

### 基础信息

- **Base URL**: `http://10.0.2.2:8081`（Android 模拟器访问宿主机）
- **Content-Type**: `application/json; charset=utf-8`
- **字符编码**: UTF-8
- **大整数格式**: 所有 `*_pool`, `reserve_*`, `my_shares_*` 字段使用**十进制字符串**传输（避免 JSON number 精度丢失）

### 3.1 健康检查

```
GET /api/gold/health
```

**响应** `200 OK`:
```json
{
  "status": "ok",
  "timestamp": 1719000000
}
```

**说明**: DApp 在每次读操作前调用（有 30 秒缓存），用于判断后端是否可用。不可用时自动回退到链上直读。

---

### 3.2 获取所有博弈池列表

```
GET /api/gold/games?user_address=0xAbCd...1234
```

**查询参数**:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_address | string | 是 | 用户钱包地址（0x 开头，42 字符） |

**响应** `200 OK`:
```json
{
  "games": [
    {
      "id": 1,
      "contract_address": "0x1234...",
      "ipfs_cid": "QmXyZ...",
      "total_pool": "5000000000000000000",
      "is_resolved": false,
      "is_refunded": false,
      "winning_option": 0,
      "deadline_sec": 1719600000,
      "reserve_yes": "3000000000000000000",
      "reserve_no": "2000000000000000000",
      "my_shares_yes": "100000000000000000",
      "my_shares_no": "0"
    }
  ]
}
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int | 博弈池 ID |
| contract_address | string | 合约地址 |
| ipfs_cid | string | IPFS CID（用于 DApp 从 IPFS 拉取元数据） |
| total_pool | string | 总资金池（wei，十进制字符串） |
| is_resolved | bool | 是否已开奖 |
| is_refunded | bool | 是否已退款 |
| winning_option | int | 获胜选项编号（0=YES, 1=NO） |
| deadline_sec | int64 | 截止时间（秒级 Unix 时间戳） |
| reserve_yes | string | YES 选项储备金（wei） |
| reserve_no | string | NO 选项储备金（wei） |
| my_shares_yes | string | 当前用户在 YES 的持仓份额（wei，0 表示无持仓） |
| my_shares_no | string | 当前用户在 NO 的持仓份额（wei） |

**说明**: DApp 拿到列表后，会使用 `ipfs_cid` 通过本地 IPFS 网关 (`http://10.0.2.2:8080/ipfs/{cid}`) 并行下载元数据 JSON，填充 desc、condition、avatarUrl 等字段。

---

### 3.3 获取单个博弈池详情

```
GET /api/gold/games/{game_id}?user_address=0xAbCd...1234
```

**路径参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| game_id | int | 博弈池 ID |

**查询参数**: 同 3.2

**响应** `200 OK`:
```json
{
  "id": 1,
  "contract_address": "0x1234...",
  "ipfs_cid": "QmXyZ...",
  "total_pool": "5000000000000000000",
  "is_resolved": false,
  "is_refunded": false,
  "winning_option": 0,
  "deadline_sec": 1719600000,
  "reserve_yes": "3000000000000000000",
  "reserve_no": "2000000000000000000",
  "my_shares_yes": "100000000000000000",
  "my_shares_no": "0"
}
```

**说明**: 字段与列表接口一致，但返回单个对象而非数组。兼容 `{"game": {...}}` 包装格式。

**错误响应** `404`:
```json
{
  "error": "博弈池不存在",
  "game_id": 999
}
```

---

### 3.4 获取用户持仓列表

```
GET /api/gold/users/{address}/positions
```

**路径参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| address | string | 用户钱包地址（0x 开头，42 字符） |

**响应** `200 OK`:
```json
{
  "positions": [
    {
      "id": 1,
      "contract_address": "0x1234...",
      "ipfs_cid": "QmXyZ...",
      "total_pool": "5000000000000000000",
      "is_resolved": false,
      "is_refunded": false,
      "winning_option": 0,
      "deadline_sec": 1719600000,
      "reserve_yes": "3000000000000000000",
      "reserve_no": "2000000000000000000",
      "my_shares_yes": "100000000000000000",
      "my_shares_no": "0"
    }
  ]
}
```

**说明**: 
- 仅返回用户 `my_shares_yes > 0 || my_shares_no > 0` 的博弈池
- 字段与列表接口一致，使用 `"positions"` 作为根键名
- 空持仓返回 `{"positions": []}`

---

### 3.5 获取历史价格数据（折线图）

```
GET /api/gold/games/{game_id}/history?contract_address=0x...&limit=256
```

**路径参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| game_id | int | 博弈池 ID |

**查询参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| contract_address | string | 是 | - | 合约地址（小写） |
| limit | int | 否 | 256 | 返回数据点数量上限 |

**响应** `200 OK`:
```json
[
  {
    "observed_at": 1719000000,
    "yes_percent": 55.32
  },
  {
    "observed_at": 1719000060,
    "yes_percent": 56.01
  }
]
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| observed_at | int64 | 观测时间（秒级 Unix 时间戳） |
| yes_percent | float64 | YES 选项的概率百分比（0-100） |

**说明**: DApp 会根据 `yes_percent` 自动计算 `no_percent = 100 - yes_percent`。

**后续优化建议**: 
- 返回 `yes_price` 和 `no_price`（代币计价），DApp 可直接使用
- 增加 `from_time` / `to_time` 查询参数支持时间范围过滤

---

### 3.6 通知后端同步（写操作后）

```
POST /api/gold/games/sync
```

**请求体**:
```json
{
  "game_id": 1,
  "contract_address": "0x1234..."
}
```

**响应** `200 OK`:
```json
{
  "status": "syncing",
  "game_id": 1
}
```

**说明**: 
- DApp 在**买入、卖出、领奖、开奖**成功后调用此接口
- 后端收到后应从链上重新读取该博弈池的最新状态并更新 DB
- 这是一个**异步通知**，DApp 不等待同步完成
- 同步失败不影响 DApp 主流程（用户下次刷新时会拿到最新数据）

---

### 3.7 批量同步（创建博弈池后）

```
POST /api/gold/games/batch-sync
```

**请求体**:
```json
{
  "contract_address": "0x1234..."
}
```

**响应** `200 OK`:
```json
{
  "status": "syncing",
  "contract_address": "0x1234..."
}
```

**说明**:
- DApp 在**创建博弈池**成功后调用此接口
- 后端应从链上重新读取该合约的所有博弈池列表并全量刷新 DB
- 创建时还没有 gameId（由合约分配），所以只传 contract_address

---

### 3.8 AI 托管状态查询

```
GET /api/gold/ai-managed?game_id=1&user_address=0xAbCd...1234
```

**查询参数**:

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| game_id | int | 是 | 博弈池 ID |
| user_address | string | 是 | 用户钱包地址 |

**响应** `200 OK`:
```json
{
  "enabled": true,
  "game_id": 1,
  "user_address": "0xabcd..."
}
```

---

### 3.9 AI 托管状态变更

```
POST /api/gold/ai-managed
```

**请求体**:
```json
{
  "game_id": 1,
  "user_address": "0xAbCd...",
  "enabled": true,
  "contract_address": "0x1234...",
  "private_key": "0xabcd..."
}
```

**说明**: 
- 后端收到 `enabled=true` 后，使用 `private_key` 为该用户在指定博弈池启动 AI 自动交易
- 生产环境应使用授权代理模式，避免传输私钥

---

## 4. 数据库表设计建议

### 4.1 博弈池表 `gold_games`

```sql
CREATE TABLE gold_games (
    id              BIGINT PRIMARY KEY,           -- 博弈池 ID（合约 gameId）
    contract_address VARCHAR(42) NOT NULL,        -- 合约地址
    ipfs_cid        VARCHAR(64) NOT NULL,         -- IPFS CID
    total_pool      DECIMAL(78,0) NOT NULL DEFAULT 0,  -- 总资金池 (wei)
    is_resolved     BOOLEAN NOT NULL DEFAULT FALSE,    -- 是否已开奖
    is_refunded     BOOLEAN NOT NULL DEFAULT FALSE,    -- 是否已退款
    winning_option  SMALLINT NOT NULL DEFAULT 0,       -- 获胜选项
    deadline_sec    BIGINT NOT NULL DEFAULT 0,         -- 截止时间（秒）
    reserve_yes     DECIMAL(78,0) NOT NULL DEFAULT 0,  -- YES 储备金 (wei)
    reserve_no      DECIMAL(78,0) NOT NULL DEFAULT 0,  -- NO 储备金 (wei)
    
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW() ON UPDATE NOW(),
    
    INDEX idx_contract_address (contract_address),
    INDEX idx_is_resolved (is_resolved),
    INDEX idx_deadline_sec (deadline_sec)
);
```

### 4.2 用户持仓表 `gold_user_positions`

```sql
CREATE TABLE gold_user_positions (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    game_id         BIGINT NOT NULL,
    user_address    VARCHAR(42) NOT NULL,
    shares_yes      DECIMAL(78,0) NOT NULL DEFAULT 0,  -- YES 份额 (wei)
    shares_no       DECIMAL(78,0) NOT NULL DEFAULT 0,  -- NO 份额 (wei)
    
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW() ON UPDATE NOW(),
    
    UNIQUE KEY uk_game_user (game_id, user_address),
    INDEX idx_user_address (user_address),
    FOREIGN KEY (game_id) REFERENCES gold_games(id)
);
```

### 4.3 历史价格表 `gold_price_history`

```sql
CREATE TABLE gold_price_history (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    game_id         BIGINT NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    observed_at     BIGINT NOT NULL,              -- 观测时间（秒级时间戳）
    yes_percent     DECIMAL(6,2) NOT NULL,        -- YES 百分比 (0.00-100.00)
    
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    
    INDEX idx_game_time (game_id, observed_at DESC),
    INDEX idx_contract (contract_address),
    FOREIGN KEY (game_id) REFERENCES gold_games(id)
);
```

### 4.4 AI 托管表 `gold_ai_managed`

```sql
CREATE TABLE gold_ai_managed (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    game_id         BIGINT NOT NULL,
    user_address    VARCHAR(42) NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    private_key     VARCHAR(128) NOT NULL,        -- ⚠️ 生产环境应使用加密存储
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW() ON UPDATE NOW(),
    
    UNIQUE KEY uk_game_user (game_id, user_address),
    FOREIGN KEY (game_id) REFERENCES gold_games(id)
);
```

---

## 5. 后端数据同步机制

### 5.1 同步策略

后端需要实现**事件驱动 + 定时轮询**的双重同步机制：

#### 方式一：监听合约事件（推荐）

监听合约的以下事件来增量更新 DB：

```solidity
event GameCreated(uint256 indexed gameId, string ipfsCID, uint256 deadlineSec);
event SharesPurchased(uint256 indexed gameId, uint8 indexed optionId, address indexed buyer, uint256 amount);
event SharesSold(uint256 indexed gameId, uint8 indexed optionId, address indexed seller, uint256 shareAmount);
event GameResolved(uint256 indexed gameId, uint8 winningOption);
event RewardClaimed(uint256 indexed gameId, uint8 indexed optionId, address indexed claimer, uint256 amount);
event GameRefunded(uint256 indexed gameId);
```

#### 方式二：DApp 主动通知（辅助）

DApp 在每次写操作成功后调用 `POST /api/gold/games/sync`，后端收到后应：
1. 调用合约 `getGameInfo(gameId)` 获取该博弈池最新状态
2. 调用合约 `getGameExtraData(gameId, userAddress)` 获取储备金和用户持仓
3. 更新 `gold_games` 表和 `gold_user_positions` 表
4. 计算新的 YES 概率并写入 `gold_price_history` 表

#### 方式三：定时全量同步（兜底）

每 30 秒调用 `getAllGames()` + `getAllGamesExtraData()` 全量刷新，作为事件监听的兜底。

### 5.2 同步流程图

```
合约事件触发 / DApp 通知 / 定时轮询
          │
          ▼
   调用合约 eth_call 读取最新状态
          │
          ▼
   更新 gold_games 表（UPSERT）
          │
          ▼
   更新 gold_user_positions 表（UPSERT）
          │
          ▼
   计算 YES% = reserve_yes / (reserve_yes + reserve_no) * 100
          │
          ▼
   写入 gold_price_history 表（INSERT）
          │
          ▼
   API 返回最新数据给 DApp
```

---

## 6. IPFS 交互规范

### 6.1 IPFS 节点配置

DApp 使用**本地 IPFS 节点**：

| 配置项 | 值 | 说明 |
|--------|-----|------|
| API 地址 | `http://10.0.2.2:5001/api/v0/add` | IPFS 写入 API |
| 网关地址 | `http://10.0.2.2:8080/ipfs/{cid}` | IPFS 读取网关 |
| 连接超时 | 5 秒 | 局域网内通信 |

### 6.2 元数据 JSON 格式

每个博弈池在 IPFS 上存储一个 JSON 文件，格式如下：

```json
{
  "desc": "2024-06-23 至 2024-06-30 黄金价格上涨",
  "condition": "黄金价格在 从 2024-06-23 到 2024-06-30 相对基准 上涨 (Price Up)",
  "avatarUrl": "QmAvatar123...",
  "detailedInfo": "Premium",
  "optionYES": "达成 (YES)",
  "optionNO": "未达成 (NO)"
}
```

**字段说明**:

| 字段 | 类型 | 说明 |
|------|------|------|
| desc | string | 博弈池标题（显示在卡片和详情页） |
| condition | string | 判定条件（显示在详情页） |
| avatarUrl | string | 头像图片的 IPFS CID（或空字符串） |
| detailedInfo | string | 详细信息（如 "Premium"） |
| optionYES | string | YES 选项的显示名称（如 "达成 (YES)"） |
| optionNO | string | NO 选项的显示名称（如 "未达成 (NO)"） |

### 6.3 图片资源

- 图片通过 `POST /api/v0/add` 以 `multipart/form-data` 上传
- `avatarUrl` 字段存储图片的 IPFS CID
- DApp 通过 `http://10.0.2.2:8080/ipfs/{cid}` 加载图片（使用 Glide 库）

### 6.4 DApp 侧的 IPFS 读取流程

```
1. 后端 API 返回 ipfs_cid
2. DApp 调用 PinataClient.downloadJsonFromIPFS(cid)
3. 使用本地 IPFS 网关 HTTP GET
4. 解析 JSON，填充 GameModel 的元数据字段
5. 如果 IPFS 读取失败，使用默认值 "博弈池 #N"
```

### 6.5 后端可选优化：缓存 IPFS 元数据

后端可以缓存 IPFS 元数据到 DB，在列表接口中直接返回，省去 DApp 的 N 次 IPFS 请求：

```sql
ALTER TABLE gold_games ADD COLUMN cached_desc TEXT;
ALTER TABLE gold_games ADD COLUMN cached_condition TEXT;
ALTER TABLE gold_games ADD COLUMN cached_avatar_url VARCHAR(128);
ALTER TABLE gold_games ADD COLUMN cached_detailed_info TEXT;
ALTER TABLE gold_games ADD COLUMN cached_option_yes VARCHAR(64);
ALTER TABLE gold_games ADD COLUMN cached_option_no VARCHAR(64);
```

如果后端缓存了 IPFS 数据，可以在列表接口中直接返回这些字段，DApp 端可以跳过 IPFS 读取步骤。但仍建议 DApp 保留 IPFS 直读能力作为回退。

---

## 7. 错误处理与回退

### 7.1 DApp 侧回退策略

```
读操作流程：
  ┌─ 后端健康检查（30 秒缓存）
  │
  ├─ 后端可用 ──→ 后端 API 读取 ──→ 成功 → 返回数据
  │                    │
  │                    └── 失败 → 回退链上 eth_call
  │
  └─ 后端不可用 ──→ 回退链上 eth_call
         │
         └── 链上 eth_call 成功 → 返回数据
         │
         └── 失败 → 返回错误给 UI

IPFS 读取（独立于后端）：
  ├─ 成功 → 填充元数据字段
  └─ 失败 → 使用默认值 "博弈池 #N"
```

### 7.2 后端 API 错误码

| HTTP 状态码 | 含义 | DApp 处理 |
|------------|------|----------|
| 200 | 成功 | 正常解析 |
| 400 | 参数错误 | 回退链上 |
| 404 | 博弈池不存在 | 回退链上 |
| 500 | 服务器内部错误 | 回退链上 |
| 503 | 服务不可用 | 回退链上 |
| 超时 (5s connect, 8s read) | 网络问题 | 回退链上 |

### 7.3 写操作错误处理

写操作直接与链交互，不依赖后端：
- 链上交易成功 → 通知后端同步（异步，失败不影响主流程）
- 链上交易失败 → 直接向用户显示错误

---

## 8. 附录：现有接口（兼容保留）

以下接口已存在，本次重构迁移至 `BackendApiClient` 统一管理，接口规范不变：

| 接口 | 方法 | 路径 | 说明 |
|------|------|------|------|
| AI 托管状态查询 | GET | `/api/gold/ai-managed?game_id=X&user_address=Y` | 保持不变 |
| AI 托管状态变更 | POST | `/api/gold/ai-managed` | 保持不变 |
| 市场历史数据 | GET | `/api/gold/market-history?contract_address=X&game_id=Y&limit=Z` | 路径改为 `/api/gold/games/{id}/history`，旧路径需保留兼容 |

---

## 变更记录

| 日期 | 版本 | 变更内容 |
|------|------|---------|
| 2026-06-23 | v1.0 | 初始版本：定义三层数据架构、RESTful API 规范、DB 表设计、IPFS 交互规范 |
