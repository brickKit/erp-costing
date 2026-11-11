# erp/costing

存货成本组件的代码。用法与边界在 `BRICKKIT.md`，为什么这样设计（术语、数据、状态、规则、未决问题）在 [docs/design.md](docs/design.md)。跨组件的规则在装配项目 be-assembly-standard 的协议 05b（存货成本：计价序列、暂估与调整、退货、输出）、协议 05（会计事实契约、不可遗漏的增量事实）、协议 06（关账，首批不封存）、协议 14（金额、纪元、列表）、安全 07（维度）与决定 D45、D46、D50。

## 代码地图

代码按业务模块分层。**先读这一节，再进目录**：每个代码目录都有自己的 `AGENTS.md`（同目录的 `CLAUDE.md` 让 Claude Code 读这个目录的文件时自动带上它），写这一层负责什么、不负责什么、每个文件、依赖谁、被谁用、测在哪；读完就知道要不要往下走。

| 目录 | 是什么 |
| --- | --- |
| [`costing/`](costing/AGENTS.md) | 入口：组件定义与组装（路由、系统面、订阅、后台工作都在这里登记，转给业务模块）；不写业务。`main.go` 与外壳只引用它 |
| [`internal/`](internal/AGENTS.md) | 业务：下面的表里每个模块一个包；模块之间的依赖表在它的 `AGENTS.md`。Go 的 `internal` 让别的组件编译期就引用不了（决定 0101） |
| [`itest/`](itest/AGENTS.md) | 集成测试，按业务流程分文件；它的 `AGENTS.md` 是测试索引（文件 → 流程 → 规则编号） |
| `costing/migrations/` | 表，整个组件一套 |
| `contract/` | 本组件发布的契约，单独的模块：`contract/proto/`、`contract/events/`（成本事实只引用 `erp/finance` 的共享会计事实契约）、`contract/openapi.yaml` |
| `cmd/seed/` | 种子（`make seed`）：角色与两个财务账号的只读范围、三个法人的计价方法（CN01 移动平均，ES01、US01 先进先出；`make seed-policies` 在库存的种子之前定），之后等计价跟上、报出各法人的范围与层；成本数据由库存、采购、销售、财务的种子经订阅形成 |
| `docs/` | 设计（规则的正文；各层的 `AGENTS.md` 只指过去，不抄） |

业务模块（`internal/` 下）：

| 模块 | 负责 | 拥有的表 |
| --- | --- | --- |
| [`sources`](internal/sources/AGENTS.md) | 更高的来源进来（LY02–LY05）：依据与撤回、票据确认与冲回，折成本位币后交给 `layers` | — |
| [`sequence`](internal/sequence/AGENTS.md) | 计价序列（VS01–VS05）：收流水、按序计价、缺口补取、纪元；计价进度的查询 | `scope`、`movement_in`、`valued_movement` |
| [`issues`](internal/issues/AGENTS.md) | 出库、采购退货、补平的成本（IS01、IS02、RT01）；系统面 `LayerService.ReturnLimit` | — |
| [`layers`](internal/layers/AGENTS.md) | 成本层与单价来源（LY01–LY05）、政策价、调整（AD01） | `cost_layer`、`layer_source`、`basis`、`basis_withdrawal`、`confirmation`、`confirmation_match`、`reversal` |
| [`methods`](internal/methods/AGENTS.md) | 计价方法的不同之处（IS01、AD02）：纯算术，换方法的替换点 | — |
| [`policies`](internal/policies/AGENTS.md) | 计价政策（PO01） | `policy` |
| [`facts`](internal/facts/AGENTS.md) | 成本事实的发布（`Publish`）与系统面 `FactService.Read` | `costing_fact`（写入在 `core.Fact`） |

模块共用的（不属于某一个业务）：[`masterdata`](internal/masterdata/AGENTS.md)（事务外取证：法人与本位币、币种的小数位、库存的补取）、[`core`](internal/core/AGENTS.md)（名字、资源与操作的定义、错误与校验、生命周期表的通用写法、增量事实 `Fact`、配置）。

**实现进度**（还没发布）：P3a 骨架；P3b 计价政策、计价序列（含等依据、政策价、移动平均出库）、计价进度的查询；P3c 来源替换与调整（依据、外币折算、票据确认与冲回、撤回）；P3e 先进先出、采购退货与可退数量、补平；P3f 成本层与成本事实的查询、恢复检查；P4 种子（`cmd/seed/`）。契约的路由全部做了。加一个模块就在上面两张表里加一行。

| 想改 | 从这里开始 | 然后 |
| --- | --- | --- |
| 一个维度、一个操作、列表能筛选的字段 | `internal/core/names.go` | `docs/design.md` 的命令与查询、`BRICKKIT.md` 的契约索引；路由的登记在 `costing/module.go` |
| 加字段、加接口、改事件 | 装配项目 be-assembly-standard 规范里的“改动配方” | 成本事实的形状属于 `erp/finance` 的共享契约，要改先改那边 |

## 构建与测试

- `go test ./...`（`make test`）：单元测试，在 `costing/contract_test.go`：清单的 `configSchema` 与 `events`、OpenAPI 与路由、合并安全。
- `make test-integration`（`go test -tags integration ./itest/`）：真实的 PostgreSQL 与消息系统；每个文件测哪条流程、覆盖哪些规则、要什么前提，在 [itest/AGENTS.md](itest/AGENTS.md)。
- `brickkit lint`

## 设计取舍

- **只按计价序号计算**：结果与消息到达的顺序无关；缺口补齐之前不往后算。
- **调整追加，不改写**：来源替换出新的成本事实，原事实不动。
- **来源不写科目**：成本事实带规则键与角色金额，财务按法人的映射出分录。

## 易错点

- 不要在事务里调库存、主数据或取汇率：先取证（`internal/masterdata`），再开事务。
- 不要用浮点算金额与数量：经 `core.Sum`、`core.Minus`、`core.Cmp`（`apd`）或在 SQL 里做；单价不舍入，金额按币种的小数位舍入。

## 改代码前自查

- 改了契约（`contract/`）：proto 改后 `make -C contract gen`，发布 `contract/v…`，再改这里的依赖；OpenAPI 与路由由测试核对。
- 改了版本号吗？每次改动都升版本。

<!-- brickkit:managed:begin lang=zh -->
## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败；`local.migrationCommand` 从源码跑同一条迁移，给 `mode: local` 用。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档——在项目里、在这个目录下跑，只查这个组件（`--all` 查整个项目）。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`；BrickKit 自己的文档用 `brickkit docs` 看。
<!-- brickkit:managed:end -->
