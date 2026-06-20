# PredictionMarket 后端配置指南

## ✨ 最新特性：XML 配置文件！

现在使用 XML 格式配置文件，程序会自动读取 `config.xml`。

---

## 📝 快速开始（3 步搞定）

### 1️⃣ 复制配置模板

```bash
cd /Users/tangyucinder/GolandProjects/PredictionMarket
cp config.example.xml config.xml
```

### 2️⃣ 编辑 config.xml 文件

用你喜欢的编辑器打开 `config.xml` 文件，填入你的私钥：

```bash
# 例如使用 vim
vim config.xml

# 或使用 nano
nano config.xml

# 或直接在 VS Code 中打开
code config.xml
```

找到这一行：
```xml
<private_key>your_private_key_here</private_key>
```

替换为你的真实私钥：
```xml
<private_key>0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890</private_key>
```

### 3️⃣ 启动后端

#### 方式 A：使用启动脚本（最简单，推荐）
```bash
./start.sh
```

#### 方式 B：直接运行（Go 会自动读取 config.xml）
```bash
go run main.go
```

#### 方式 C：运行编译好的二进制
```bash
./bin/sentinel
```

---

## 🎯 就是这么简单！

程序启动时会自动：
1. 检查当前目录下的 `config.xml` 文件
2. 加载所有配置项
3. 验证配置是否正确

---

## 📄 XML 配置文件说明

```xml
<?xml version="1.0" encoding="UTF-8"?>
<config>
    <!-- 必需：你的钱包私钥（用于签名开奖交易） -->
    <private_key>your_private_key_here</private_key>

    <!-- 合约地址（可选，有默认值） -->
    <contract_address>0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c</contract_address>

    <!-- RPC URL（留空则使用 BrokerChain） -->
    <rpc_url></rpc_url>

    <!-- BrokerChain API 地址 -->
    <broker_chain_url>https://dash.broker-chain.com:443/</broker_chain_url>

    <!-- IPFS 网关地址 -->
    <ipfs_gateway>http://127.0.0.1:8080/ipfs/</ipfs_gateway>

    <!-- 扫描间隔（秒） -->
    <poll_interval>30</poll_interval>

    <!-- 判定后延迟（秒） -->
    <resolve_delay>5</resolve_delay>

    <!-- 是否使用 BrokerChain（true/false，可选，会自动判断） -->
    <use_broker_chain></use_broker_chain>
</config>
```

---

## 🔑 关于私钥

### 什么是私钥？
私钥是你的钱包唯一凭证，用于：
- 签名区块链交易
- 证明你是交易的发起者
- 调用合约的 `resolveGame()` 函数进行开奖

### 如何获取私钥？

**从钱包导出（例如 MetaMask）：**
1. 打开 MetaMask
2. 点击账户详情
3. 选择 "导出私钥"
4. 输入密码验证
5. 复制私钥（以 0x 开头或不带 0x 都可以）

**从 Android 项目获取：**
查看 `agent` 目录下的代码，找到私钥配置的位置。

### ⚠️ 安全警告

- **永远不要**将 `config.xml` 文件提交到 Git（已在 `.gitignore` 中保护）
- **永远不要**在公开场合分享私钥
- 建议使用专门用于开奖的钱包，不要存大额资金

---

## ⚙️ 所有配置项说明

| 配置项 | XML 标签 | 必需 | 默认值 | 说明 |
|--------|----------|------|--------|------|
| 私钥 | `private_key` | ✅ 是 | - | 钱包私钥 |
| 合约地址 | `contract_address` | ❌ 否 | `0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c` | 博弈池合约地址 |
| RPC URL | `rpc_url` | ❌ 否 | - | 本地区块链 RPC 地址（留空则使用 BrokerChain） |
| BrokerChain URL | `broker_chain_url` | ❌ 否 | `https://dash.broker-chain.com:443/` | BrokerChain API 地址 |
| IPFS 网关 | `ipfs_gateway` | ❌ 否 | `http://127.0.0.1:8080/ipfs/` | IPFS 网关地址 |
| 扫描间隔 | `poll_interval` | ❌ 否 | `30` | 扫描间隔（秒） |
| 延迟时间 | `resolve_delay` | ❌ 否 | `5` | 判定后延迟（秒） |
| 使用 BrokerChain | `use_broker_chain` | ❌ 否 | 自动判断 | 强制使用 BrokerChain（true/false） |

---

## 🧪 测试配置是否正确

设置好私钥后，直接运行验证：

```bash
go run main.go
```

如果配置正确，你会看到：
```
time=xxx level=INFO msg="loading config from XML file" file="config.xml"
time=xxx level=INFO msg="prediction market sentinel started" contract=xxx wallet=xxx ...
```

---

## 🐛 常见问题

### Q: 提示 "config file config.xml not found"
**A:** 配置文件不存在。请确保：
1. 已复制 `config.example.xml` 为 `config.xml`
2. `config.xml` 文件在项目根目录下

### Q: 提示 "private_key is required"
**A:** 私钥没有设置或使用了默认值。请确保：
1. 已在 `config.xml` 中填入真实私钥
2. 没有使用 `your_private_key_here` 这个默认值

### Q: 私钥需要带 0x 前缀吗？
**A:** 都可以。代码会自动处理。

### Q: 如何确认后端在正常运行？
**A:** 查看日志输出，应该会看到：
- `loading config from XML file`（成功加载配置）
- 定期扫描的日志
- `scan complete` 信息
- 如果有待开奖的博弈池，会显示判定和交易信息

### Q: 可以在多个终端同时运行吗？
**A:** 可以，但不推荐。`resolving` 同步 map 会防止重复处理同一个博弈池。
