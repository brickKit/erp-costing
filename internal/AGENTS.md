# internal/：业务模块

存货成本的业务，每个模块一个包。只给本仓库引用（Go 的 `internal`）：别的组件编译期就引用不了，跨组件只走契约（决定 0101）。

先按下表找到模块（按层从高到低排），再读那个目录的 `AGENTS.md`。

## 模块

| 包 | 一句话 | 依赖（本组件内） |
| --- | --- | --- |
| `sources` | 更高的来源进来：依据与撤回、票据确认与冲回；折成本位币后交给 `layers` | `core`、`masterdata`、`policies`、`layers`、`sequence` |
| `sequence` | 计价序列：收流水、按序号一条一条计价、缺口补取、纪元、停下；计价进度的生命周期与查询 | `core`、`masterdata`、`policies`、`layers`、`issues` |
| `issues` | 出库、采购退货、补平的成本；系统面的可退数量 | `core`、`facts`、`layers`、`methods`、`policies` |
| `layers` | 成本层与单价来源：入层、政策价、来源的记录与替换、调整（AD01） | `core`、`facts`、`methods` |
| `policies` | 计价政策：法人的计价方法 | `core`、`masterdata` |
| `methods` | 计价方法的不同之处（纯算术）：移动平均出库取多少、调整怎样分（AD02） | `core` |
| `facts` | 成本事实的形状与发布（`Publish`）；系统面 `FactService.Read` | `core` |
| `masterdata`（共用） | 事务外取证：法人与本位币、币种的小数位、库存的补取（`Movements`） | `core` |
| `core`（共用） | 名字、资源与操作的定义、错误与校验、生命周期表、增量事实、配置、`Base` | — |

分层（每个包只引用比它低的层，没有环）：

| 层 | 包 |
| --- | --- |
| 6 | `sources` |
| 5 | `sequence` |
| 4 | `issues` |
| 3 | `layers` |
| 2 | `facts`、`masterdata`、`methods`、`policies` |
| 1 | `core` |

核对：`go list -f '{{.Imports}}' ./internal/<包>`。

## 共同的写法

- **每个模块的函数先收 `m *core.Base`**（组件的运行时与配置），入口包的 `Module` 持有它、登记时适配；模块之间直接调对方导出的函数。
- **名字在 `core`**：资源、操作、维度、事件类型、依赖的 ID、错误原因都在 `core/names.go`、`core/rules.go`；资源定义（`core.Definitions()`）是全组件一张表。
- **生命周期是一张表**：计价进度与成本层的单价来源在哪个状态做什么，只在各自模块的 `lifecycle.go`（`core.Table`，写法照 `erp/purchase`，SF30：以后进 SDK）。
- **增量事实只经 `core.Fact`**：发事件与存进 `costing_fact` 在同一个事务。
- **事务外取证、事务内写**：上游调用（库存的补取、主数据、汇率）在事务之前；写入、审计（`core.Change`）与发件表在同一个事务。

## 加一个模块

一个新的业务能力有自己的表、路由与规则时才建模块（拿不准时放进最近的模块，宁可少切）：建目录、写 `AGENTS.md` 与 `CLAUDE.md`（`@AGENTS.md`），把它加进上面的表与仓库根的模块表，在 `costing/module.go` 登记它的路由与工作。

## 测试

模块没有单独的单元测试（生命周期表与纯算术的 `methods` 除外）：业务规则由 `itest/` 的流程测试覆盖，每个模块的说明写了它在哪个文件里被测。
