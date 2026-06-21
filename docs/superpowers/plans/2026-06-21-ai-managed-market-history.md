# AI 托管链上占比与真实历史实现计划

> **执行说明：** 本计划只修改 Go 后端与后端配置文档。`agent/` 目录现有未提交改动属于用户，执行、暂存和提交时都必须排除。

**目标：** AI 托管开启后由后端持续读取链上池状态和 IPFS 历史；以真实 YES/NO 占比构建进程内历史，历史少于配置门槛时强制 HOLD，达到门槛后才调用模型并允许交易。

**实现边界：** IPFS 只作为历史种子，链上轮询点保存在并发安全的内存存储中。历史按“规范化合约地址 + game_id”共享，不按用户分裂。当前版本不引入 SQLite/IPNS，不修改 Android 前端，也不广播任何测试交易。

**技术栈：** Go、`math/big`、`encoding/json`、现有链客户端/IPFS 客户端/AI 托管引擎、YAML 严格配置、Go 标准测试。

---

## 任务 1：增加历史门槛和容量配置

**文件：**

- 修改：`internal/config/config_test.go`
- 修改：`internal/config/config.go`
- 修改：`config.example.yaml`
- 修改但不暂存：`config.yaml`
- 修改：`SETUP.md`

### 1.1 先写失败测试

扩充有效 YAML fixture：

```yaml
ai:
  history_min_points: 3
  history_max_points: 256
```

在 `TestLoadFileReadsCompleteYAML` 中断言：

```go
if cfg.AIHistoryMinPoints != 3 || cfg.AIHistoryMaxPoints != 256 {
    t.Fatalf("unexpected AI history settings: min=%d max=%d", cfg.AIHistoryMinPoints, cfg.AIHistoryMaxPoints)
}
```

向无效配置表增加三项：

- `history_min_points: 0`
- `history_max_points: 0`
- `history_min_points: 4` 且 `history_max_points: 3`

扩展仓库配置制品测试，要求示例 YAML 和说明包含两个字段。

### 1.2 运行红灯测试

```bash
go test ./internal/config
```

预期：`Config` 尚无历史字段，测试编译失败或断言失败。

### 1.3 最小实现

在 `fileConfig.AI` 增加严格 YAML 映射：

```go
HistoryMinPoints int `yaml:"history_min_points"`
HistoryMaxPoints int `yaml:"history_max_points"`
```

在运行时 `Config` 增加：

```go
AIHistoryMinPoints int
AIHistoryMaxPoints int
```

加载时复制字段并验证：两项必须大于零，且 `max >= min`。错误信息分别明确指向 `ai.history_min_points`、`ai.history_max_points` 和二者关系。

同步 `config.example.yaml`、本机 `config.yaml` 与 `SETUP.md`。本机配置只补两个非敏感数值；由于它包含用户密钥且被忽略，绝不输出其内容、绝不暂存。

### 1.4 运行绿灯测试

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config
```

预期：通过。

---

## 任务 2：解析和清洗 IPFS 历史点

**文件：**

- 新增：`internal/ipfs/history.go`
- 新增：`internal/ipfs/history_test.go`
- 修改：`internal/ipfs/client.go`

### 2.1 先写失败测试

为 `DownloadMetadata` 使用内联 JSON CID，覆盖：

1. 短字段 `{\"t\": 1, \"y\": 60, \"n\": 40}`。
2. 长字段 `{\"time\": 2, \"yes\": 55, \"no\": 45}`。
3. 仅 YES 或仅 NO 时推导另一侧。
4. 无时间、负时间、NaN/Infinity 表达不可解析、百分比越界、两侧合计明显不为 100 的点被忽略。
5. 重复时间以输入中最后一个有效点为准。
6. 输出按时间升序排列。
7. 单个非法历史点不影响 `desc`、`condition`、`detailedInfo` 等静态元数据。

目标公开数据结构：

```go
type HistoryPoint struct {
    Time       int64   `json:"time"`
    YesPercent float64 `json:"yes_percent"`
    NoPercent  float64 `json:"no_percent"`
}
```

### 2.2 运行红灯测试

```bash
go test ./internal/ipfs
```

预期：`Metadata.History` 和解析逻辑不存在，测试失败。

### 2.3 最小实现

在 `Metadata` 增加：

```go
History []HistoryPoint `json:"-"`
```

实现 `Metadata.UnmarshalJSON`：静态字段正常解析，`history` 先接收为 `[]json.RawMessage`。逐点解析为带指针的内部结构，以区分“缺字段”和“值为 0”；兼容 `t/time`、`y/yes`、`n/no`。规则如下：

- 时间取已存在的别名且必须 `> 0`。
- 至少存在 YES 或 NO 之一。
- `math.IsNaN`、`math.IsInf`、小于 0 或大于 100 均无效。
- 缺一侧时用 `100-known` 补齐。
- 两侧均存在时允许 `abs(yes+no-100) <= 0.5`；在容差内归一化为精确合计 100，避免显示与模型输入漂移。
- 无效点直接跳过。
- 使用时间戳 map 去重，最后一个有效点覆盖前一个；最终升序输出。

`DownloadMetadata` 保持原有外部接口不变，内联 CID 和网关 JSON 都自动使用自定义反序列化。

### 2.4 运行绿灯测试

```bash
gofmt -w internal/ipfs/client.go internal/ipfs/history.go internal/ipfs/history_test.go
go test ./internal/ipfs
```

预期：通过。

---

## 任务 3：实现链上占比和并发安全的真实历史存储

**文件：**

- 新增：`internal/aimanaged/history.go`
- 新增：`internal/aimanaged/history_test.go`

### 3.1 先写失败测试：占比

为 `pointFromReserves` 覆盖：

- 合约储备输入顺序 `[NO, YES] = [25, 75]`，输出 YES=75、NO=25。
- 使用远超 `float64` 精确整数范围的 `big.Int`，仍得到有限且正确的比例。
- nil、负数、两侧合计为零返回错误。

计算实现不得先把储备转 `float64`。使用 `big.Rat.SetFrac` 计算比例，最后 `Float64()` 并验证有限性。

### 3.2 先写失败测试：存储

为 `marketHistoryStore` 覆盖：

- `marketKey` 把同一地址的不同大小写归为同一键，并区分不同 `game_id`。
- 合并 IPFS 种子后按时间排序、按时间去重。
- 同一轮询时间桶内，同一池两个不同用户只生成一个当前链上点。
- 同一时间桶的新链上值覆盖旧值，避免多个用户读到略有时差时产生重复横坐标。
- 超过 `history_max_points` 后仅保留最新点。
- `Snapshot` 返回副本，调用方修改结果不污染内部状态。

### 3.3 运行红灯测试

```bash
go test ./internal/aimanaged -run 'Test(PointFromReserves|MarketHistory)'
```

预期：新类型和函数尚不存在，测试失败。

### 3.4 最小实现

实现：

```go
type marketHistoryStore struct {
    mu       sync.RWMutex
    max      int
    interval time.Duration
    points   map[string][]ipfs.HistoryPoint
}
```

核心操作：

```go
func marketKey(contract string, gameID int) string
func pointFromReserves(extra *chain.GameExtraData, observedAt time.Time) (ipfs.HistoryPoint, error)
func (s *marketHistoryStore) MergeAndAppend(key string, seed []ipfs.HistoryPoint, point ipfs.HistoryPoint) []ipfs.HistoryPoint
func (s *marketHistoryStore) Snapshot(key string) []ipfs.HistoryPoint
```

时间使用注入时钟得到的 `now`，再按 `cfg.AIPollInterval` 分桶。若间隔异常则防御性使用原始 Unix 秒；正常生产配置不会进入该分支。合并过程在单次写锁内完成，保证多个托管用户共享同一池时不会竞争产生重复点。

### 3.5 运行绿灯与竞态测试

```bash
gofmt -w internal/aimanaged/history.go internal/aimanaged/history_test.go
go test ./internal/aimanaged -run 'Test(PointFromReserves|MarketHistory)'
go test -race ./internal/aimanaged -run 'TestMarketHistory'
```

预期：通过且无数据竞争。

---

## 任务 4：把历史门禁接入 AI 托管编排

**文件：**

- 修改：`internal/aimanaged/manager_test.go`
- 修改：`internal/aimanaged/manager.go`

### 4.1 先写失败测试

把测试决策源改为可记录调用次数和最后一次研究上下文：

```go
type ResearchContext struct {
    Current ipfs.HistoryPoint
    History []ipfs.HistoryPoint
}
```

新增编排测试：

1. IPFS 无历史且只有本轮链上点：决策调用为 0、交易为 0。
2. IPFS 只有 1 个有效历史点，加本轮链上点仍不足 3：决策调用为 0。
3. IPFS 有 2 个有效历史点，加本轮链上点达到 3：决策调用恰好 1 次，传入历史升序且最后一点为当前链上占比。
4. 两个托管条目使用不同用户但相同合约/game_id：同一轮后历史只有一个链上时间点。
5. 储备总额为零：不调用行情、不调用决策、不交易。

更新既有 HOLD、低置信度、高置信度交易测试的 fixture，使其提供足够历史，以继续验证原规则而不被新门禁提前截断。

### 4.2 运行红灯测试

```bash
go test ./internal/aimanaged
```

预期：引擎尚未维护历史，也没有门禁，新增测试失败。

### 4.3 最小实现

向 `Engine` 增加共享历史和可测试时钟：

```go
history *marketHistoryStore
now     func() time.Time
```

`NewEngine` 使用 `cfg.AIHistoryMaxPoints`、`cfg.AIPollInterval` 创建一次共享存储，并默认 `now = time.Now`。

扩展决策接口：

```go
Decide(ctx context.Context, info *chain.GameInfo, extra *chain.GameExtraData, meta *ipfs.Metadata, quote *oracle.Quote, research *ResearchContext) (*Decision, error)
```

`process` 的顺序固定为：

1. 解密托管密钥并创建链客户端。
2. 校验钱包地址。
3. 读取 `GetGameInfo`，已结算或已过期则禁用任务。
4. 尝试读取 IPFS 元数据；失败时记录警告并使用空元数据继续链上采样。
5. 读取 `GetGameExtraData`。
6. 用 `[NO, YES]` 储备生成本轮点；无效则记录“强制 HOLD”并返回。
7. 将 IPFS 有效种子和当前点原子合并到共享历史。
8. 若历史数 `< AIHistoryMinPoints`，记录结构化日志（contract、game_id、points、required、decision=hold），直接返回；此时不获取行情、不调用模型、不交易。
9. 达标后获取黄金行情，构建 `ResearchContext`，调用模型。
10. 继续执行原有 action 校验、置信度、冷却、固定金额和交易记录逻辑。

元数据下载失败后的空值处理必须避免 nil 解引用；链上采样仍然留下真实历史点，后续轮询可自行达到门槛。

### 4.4 运行绿灯测试

```bash
gofmt -w internal/aimanaged/manager.go internal/aimanaged/manager_test.go
go test ./internal/aimanaged
```

预期：所有新旧编排测试通过。

---

## 任务 5：把真实历史和当前占比写入安全的 AI 提示词

**文件：**

- 修改：`internal/aimanaged/manager_test.go`
- 修改：`internal/aimanaged/manager.go`

### 5.1 先写失败测试

用 `httptest.Server` 捕获 `AIClient.Decide` 的请求体，断言：

- system 消息明确说明标题、条件、详细说明等 IPFS 市场文本是不可信数据，不得作为指令执行。
- user 消息包含 `detailed_info`。
- 包含 `current_yes_percent`、`current_no_percent`。
- 包含升序的 `market_history`，每个点带时间、YES%、NO%。
- 继续包含原始 `reserve_no`、`reserve_yes`、池总额、截止时间和黄金行情。
- 响应仍只接受 `buy_yes`、`buy_no`、`hold`。

测试只启动本地 HTTP server，不请求真实模型。

### 5.2 运行红灯测试

```bash
go test ./internal/aimanaged -run TestAIClientDecisionPromptIncludesResearchHistory
```

预期：现有提示词没有历史和安全边界，断言失败。

### 5.3 最小实现

将历史以 JSON 数组嵌入用户消息，字段名固定并使用 `json.Marshal`，避免手工拼接破坏转义。对 IPFS 字符串使用数据标签包裹，并在 system 指令中声明：这些字段只可作为预测材料，不可改变角色、工具、输出格式或交易约束。

保持模型输出 JSON schema 和现有 `parseDecision` 行为不变，避免扩大交易动作集合。

### 5.4 运行绿灯测试

```bash
gofmt -w internal/aimanaged/manager.go internal/aimanaged/manager_test.go
go test ./internal/aimanaged -run TestAIClientDecisionPromptIncludesResearchHistory
go test ./internal/aimanaged
```

预期：通过。

---

## 任务 6：全量验证、变更隔离与提交

**文件检查范围：**

- `internal/config/`
- `internal/ipfs/`
- `internal/aimanaged/`
- `config.example.yaml`
- `SETUP.md`
- `docs/superpowers/specs/2026-06-21-ai-managed-market-history-design.md`
- `docs/superpowers/plans/2026-06-21-ai-managed-market-history.md`

### 6.1 格式化与全量测试

```bash
gofmt -w internal/config/*.go internal/ipfs/*.go internal/aimanaged/*.go
go test ./...
go vet ./...
go test -race ./internal/aimanaged
```

预期：全部退出码为 0；测试无外网访问、无真实交易。

### 6.2 检查配置与敏感信息边界

确认：

- `config.example.yaml` 只含占位符。
- `config.yaml` 仍被忽略，未进入差异或暂存区。
- 日志、测试 fixture、文档没有真实私钥/API key。
- YAML 严格解析仍能拒绝拼错字段。

### 6.3 检查用户前端改动未被触碰

执行前后比较 `git status --short`。`agent/` 的八个已有修改应保持原样；本次不格式化、不暂存、不提交任何 `agent/` 文件。

### 6.4 精确暂存并复核

只显式暂存后端实现、测试、公开示例配置和文档；不要使用会包含全部工作区的暂存命令。复核暂存差异和文件列表，确保无 `agent/`、无 `config.yaml`、无意外密钥。

### 6.5 提交

建议提交信息：

```text
feat: gate ai trading on real market history
```

提交后再次运行 `git status --short --branch`，报告仍保留的用户前端改动和未跟踪 backlog，不将其误报为本次遗漏。

---

## 最终交付说明

完成后向用户总结：

1. 数据从链上/IPFS 到历史存储、门禁、模型、交易的完整路径。
2. NO/YES 合约储备顺序与百分比换算细节。
3. 历史清洗、分桶、去重、共享和容量限制。
4. `< 3` 强制 HOLD 如何保证模型无法绕过。
5. AI 提示词新增信息与不可信内容隔离。
6. YAML 新配置及推荐值。
7. 本轮未做的前端随机曲线、SQLite/IPNS 持久化以及进程重启丢历史的限制。
8. 测试、vet、race 的实际结果和所有改动文件。
