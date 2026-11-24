# 枢纽机场行李报文中间件与一日运行模拟

用 Go 标准库实现的行李中间件 / BHS 模拟系统。系统生成一个枢纽机场一天的航班、旅客、托运行李、中转行李和异常事件，并通过内嵌 Web 页面实时展示分拣线、出口、航班装载清单、集装箱层位、告警、报文流和验收指标。

> 确定性种子：`16252`。一次完整日模拟约生成 **2.4 万名旅客、3.7 万件行李/标签、16 万条中间件报文**。`go run ./cmd/verify` 通常在数秒内跑完一整天。

## 快速开始

```bash
# 启动页面和 API，默认 :8080
go run ./cmd/server

# 浏览器打开
open http://localhost:8080
```

可选参数：

```bash
go run ./cmd/server -addr 127.0.0.1:18080 -seed 42
```

页面支持：

- 暂停 / 继续；
- `6×`、`18×`、`60×` 模拟时钟速度；
- “快进到日终”；
- 重新生成同一天；
- 搜索航班号、目的地、登机口；
- 点击航班查看装载清单、集装箱、层/货位、关舱核对和每件行李的完整轨迹；
- 直接打开航班详情：`http://localhost:8080/#flight=OUT019`。

无前端构建步骤，HTML/CSS/JS 通过 `go:embed` 编入二进制。

## 命令行验收

```bash
# 确定性跑完一天并打印验收报告；若有错误行李、漏检问题航班或中转失败则非零退出
go run ./cmd/verify

# JSON 指标
go run ./cmd/verify -json

# 完整测试 / 竞态检查
go test ./...
go test -race ./...
```

一次典型报告：

```text
旅客 24364，行李报文/标签 37023，报文总量 166538
最终去向正确：37023/37023，正确率 100.00%（错误 0，未审计 0）
未登机定位：18 件，准确 18，准确率 100.0%；平均取出 76.7 秒，最大 99.0 秒；盲找基线 2400 秒
中转行李：10003，原航班衔接 9643，自动保护改签 358，最终成功 10001，取消 2，失败 0，最终成功率 100.0%
关舱核对：植入问题 6，发现 6，漏检 0；完成核对 157 班，放行 157 班，起飞 157 班
异常标签：扫描 12，核实恢复 9；换口 2 次，换机 1 次，改签旅客 359，取消旅客 1
```

说明：

- “未登机定位”按产生的定位/卸载任务件数统计；多名旅客有多件行李时，件数大于脚本中的重点旅客人次。
- 两件中转行李属于临时取消旅客，因此不计入衔接失败。
- 实时页面在一天尚未结束时，“在途/未审计”数量会较大；日终才会形成全部最终结论。

## 需求与实现对照

| 需求 | 实现方式 | 页面/验证 |
|---|---|---|
| 1. 值机生成报文；扫码按报文决定出口；有签无报文送异常出口并提醒值机核实 | `CheckIn` 发布 BSM；`ScanBag` 按当前航班/出口路由；`HandleUnknownPhysicalScan` 创建未知标签聚合，送 `EXC` 并产生 critical 告警；`ResolveUnknownScan` 补报后重新路由 | 分拣线红点、`EXC` 出口、告警流、`SC-ORPHAN` 场景 |
| 2. 换登机口/飞机：未装车改新出口，已装车通知装卸 | `ChangeGate` / `ChangeAircraft` 遍历该航班行李；未装件更新 `ChuteID/ExpectedChuteID` 并追加路由事件；已装件保留集装箱记录并生成装卸/配载通知 | 航班详情中的变更记录、告警、报文 `FLIGHT_GATE_CHANGED` / `FLIGHT_AIRCRAFT_CHANGED` |
| 3. 每件行李记录集装箱、层、货位；未登机时立即精确指引 | `LoadBag` 分配集装箱和层位，记录 `ContainerCode/Layer/LayerLabel/Slot`；`LocatePassengerBags` 生成预测位置和处理用时；`CompleteRetrievals` 核对预测与实际位置并卸载 | 点击航班可见集装箱层计数和每件行李货位；KPI 显示定位准确率/平均用时 |
| 4. 中转行李按 MCT 判断；来不及则保护下一班并通知旅客；旅客改签行李跟随 | `AssessTransfer` 按国内/国际最短衔接时间和预计起飞判断；无定期航班时 `CreateRecoveryFlight` 生成补班；`RebookPassenger` 同步更新旅客、行李、出口和预期去向，并发送通知 | 中转指标、旅客通知告警、行李轨迹、动态补班航班 |
| 5. 取消/撤销值机作用于分拣线上的行李 | `CancelPassenger` 对线上行李置 `recall_chute`、改派 `RCL`，随后 `CompleteRecall` 作废；已装车则生成精确卸载任务 | `SC-CANCEL`、琥珀色召回点、`RCL` 出口、行李轨迹 |
| 6. 关舱前三方核对，不一致禁止放行 | `ReconcileFlight` 核对：已登机旅客的行李是否在机、未登机/取消旅客的行李是否仍在机、实物集装箱与报文航段是否一致；失败时 `ReleaseBlocked=true`，引擎自动处置并复核，通过后才允许 `DepartFlight` | 航班详情红色/绿色核对面板、问题明细、处置建议；指标显示植入问题全部发现、漏检 0 |

系统还会主动植入 6 类人为/装卸缺陷：漏卸未登机行李、错装其他航班/集装箱、已登机行李实物或舱单丢失。关舱核对必须全部拦下并完成修复复核。

## 页面说明

### 分拣线和出口

- 蓝色：普通行李在分拣线上；
- 紫色：中转行李；
- 绿色：已按报文分配出口；
- 红色：扫到标签但找不到报文；
- 琥珀色：取消/撤销后的召回行李。

出口卡片显示当前关联航班、目的地、登机口和当前在流件数。`EXC` 是无报文异常出口，`RCL` 是撤销召回出口。

### 航班详情

点击航班表任意一行，或访问 `/#flight=航班ID`：

- 计划/预计/登机/核对/起飞时间；
- 登机口、出口、机型机号；
- 关舱核对结果和所有不一致明细；
- 每个集装箱/散舱的顶/中/底层件数；
- 每件行李的标签、旅客、状态、当前/原始航班、集装箱、层、货位；
- 点击“轨迹”查看完整事件链。

## HTTP API

| 方法/路径 | 说明 |
|---|---|
| `GET /api/overview` | 时钟、全部 KPI、航班、出口、分拣线活动行李、最近告警/报文、场景状态 |
| `GET /api/flights/{id}` | 航班、旅客、行李、集装箱和最近一次核对记录 |
| `GET /api/bags/{tag}` | 单件行李完整聚合和轨迹 |
| `POST /api/control` | `{"action":"play|pause|toggle|reset|finish|speed","speed":60}` |
| `GET /api/health` | 健康检查 |

示例：

```bash
curl -s http://localhost:8080/api/overview | jq '.stats'
curl -s http://localhost:8080/api/flights/OUT019 | jq '.containers'
curl -X POST http://localhost:8080/api/control \
  -H 'Content-Type: application/json' \
  -d '{"action":"speed","speed":60}'
```

## 代码结构

```text
cmd/server/             Web/API 启动入口
cmd/verify/             无浏览器的一日验收报告
internal/domain/        航班、旅客、行李、集装箱、核对、指标等领域模型
internal/middleware/    内存消息总线（主题订阅/发布）
internal/store/         带 RWMutex 的内存仓储
internal/hub/           行李中间件核心规则：BSM、路由、装卸、改签、核对、审计
internal/sim/           确定性数据生成、场景导演、时钟推进和测试
internal/metrics/       KPI 汇总
internal/api/           HTTP API 与 embed 的前端资源
internal/api/web/       原生 HTML/CSS/JS 运维大屏
```

## 模拟时钟和并发

- 模拟时间以“分钟数（从 00:00 起）”表示，不依赖宿主机日期；
- Server 每 100ms 推进一步，速度表示“每个真实秒对应多少模拟分钟”；
- 所有共享聚合在 `store.Store.Mu` 后访问；
- HTTP JSON 编码期间持有读锁，避免读取到半更新状态；
- 消息总线同步分发给监控/审计订阅者，`Service.RecordMessage` 同时保存最近/全量报文记录。

## 可继续扩展的方向

- 将内存总线替换为 Kafka/NATS，将仓储替换为 PostgreSQL/Redis；
- 增加 BSM/BPM/CPM/AHM 等真实报文格式编解码；
- 接入读取器、PLC、分拣机和手持 PDA 的 WebSocket/GRPC 模拟器；
- 增加集装箱载重/平衡、超规行李、海关安检和容量约束；
- 为不同机场配置 MCT、出口映射、集装箱数量和保障半径。
