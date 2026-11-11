# itest/：集成测试与测试索引

存货成本组件对着真实的 PostgreSQL 与消息系统、经它真实的接口（用户面 HTTP、系统面 gRPC、事件）的测试（L2）。构建标签 `integration`；一条业务流程一个文件。

## 怎样跑

- `make test-integration`（`go test -count=1 -tags integration ./itest/`）。
- 环境变量：`BE_SDK_TEST_PG_ADMIN`（口令是装配项目 `.env` 的 `POSTGRES_PASSWORD`）、`BE_SDK_TEST_NATS_URL=nats://localhost:4222`；be-sdk-go 的 `sectest` 是测试部署。
- 前提：装配项目里 `make up` 起了 `be-postgres` 与 `be-nats`。
- 端口：28229（用户面）、9229（系统面）、替身 19140（`mdm/org`）、19141（`mdm/currency`）、19142（`erp/inventory` 的 `StockService.Movements`）要空着，被占时测试直接失败。
- 每次开始：删掉再建自己的 schema `erp_costing_it`（跑完留着，可以查）；删掉流 `erp.inventory`、`erp.purchase`、`erp.finance`、`erp.costing`（别的运行留下的事件）。
- 配置：两个等待都是 2 秒（`VALUATION_GAP_WAIT`、`VALUATION_BASIS_WAIT`）。
- **只有一个测试** `TestCostingEndToEnd`：它按下表的顺序跑各步；改了哪个流程就跑整个。

## 文件

按运行顺序：

| 文件 | 步 | 测的流程 | 覆盖的规则 | 被测的模块 |
| --- | --- | --- | --- | --- |
| `policy_integration_test.go` | `policies` | 计价政策的缺省、按版本修改、错的方法、不认识的法人 | PO01 | `policies` |
| `sequence_integration_test.go` | `sequence` | 按依据入库与入库事实、乱序到达等缺口、移动平均出库与事实、重复的流水、缺口到期补取、补不到的缺口再等时保留等待的起点、等依据与依据到达、依据不来按政策价、非采购入库与价格为 0、晚装的范围补前面的号、计价进度的读取与列表 | VS01–VS03、LY01、IS01、CF01 | `sequence`、`layers`、`issues`、`facts` |
| `sequence_integration_test.go` | `unknownKind` | 不认识的流水种类停下范围，之后的流水收下不计价 | VS02、VS05 | `sequence` |
| `sequence_integration_test.go` | `epochs` | 换纪元：先把旧纪元补到底再从 1 起；重新编号的流水不再计价；更旧纪元的丢弃；本组件超前时停下 | VS04 | `sequence` |
| `sources_integration_test.go` | `sources` | 依据替换政策价与移动平均的分法、同一依据再来、外币依据折算、两张票据各占自己的数量（供应商、票据日期、外币）、出清后冲回全进差异、冲回先于确认、确认先于入库（按票据价入层不调整）、开票后到的依据只换没开票的单位、撤回的依据 | LY02–LY05、AD01、AD02（移动平均） | `sources`、`layers`、`methods` |
| `issues_integration_test.go` | `fifo` | 北京改先进先出：按单位单价出库、开票后的调整按已出库的单位分、可退数量与采购退货、取走全部、在库不够与之后入库补平 | IS01、IS02、RT01、AD02（先进先出） | `issues`、`layers`、`methods` |
| `issues_integration_test.go` | `returns` | 移动平均的采购退货（层的加权单价、不限数量）、补平更正销售成本、只许采购问可退数量 | RT01、IS02 | `issues` |
| `queries_integration_test.go` | `queries` | 成本层的读取与来源、先进先出的在库层、层的列表（来源、在库、翻页）、成本事实的列表与触发、读不存在的答 404 | 命令与查询、CF01 | `layers`、`facts`、`sequence` |
| `queries_integration_test.go` | `recoveryChecks` | 只许恢复工具；计价政策的存在性与版本；停下、为负、久等的范围 | 整套恢复之后、VS05、IS02 | 入口 `checks.go`、`sequence` |
| `policy_integration_test.go` | `policyInUse` | 有计价过的流水后不能改方法 | PO01 | `policies` |

共用的：

| 文件 | 内容 |
| --- | --- |
| `endtoend_integration_test.go` | `TestCostingEndToEnd`（各步的顺序）；`fixture`（运行中的成员、替身、世界的 ID：上海与北京两个法人、一个仓库、一个供应商）；`start`；辅助：`publish`（在来源的流上发布事实，像它的转发器那样）、`fact`、`client.do`、`at`、`get`、`portFree` |
| `standins_integration_test.go` | 替身，各按已发布的契约经 mTLS 答：`mdm/org`（法人）、`mdm/currency`（CNY 与 USD；即期汇率 USD→CNY 7.2）、`erp/inventory` 的 `StockService.Movements`（测试编好号的流水，按纪元与序号答；`count` 数调用） |

## 加测试

- 新流程：新文件 `<流程>_integration_test.go`，写一个 `func (fx *fixture) <步>()`，在 `TestCostingEndToEnd` 里按依赖排进顺序，在上表加一行。
- 测试不为了通过而改（装配项目规范的铁律）。
