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
| `unsupported_integration_test.go` | `notInThisVersion` | 契约里有、还没做的路由答“能力不支持” | D75 | 入口 `costing/module.go` |

共用的：

| 文件 | 内容 |
| --- | --- |
| `endtoend_integration_test.go` | `TestCostingEndToEnd`（各步的顺序）；`fixture`（运行中的成员、替身、世界的 ID：上海与北京两个法人）；`start`；辅助：`publish`（在来源的流上发布事实，像它的转发器那样）、`fact`、`client.do`、`at`、`get`、`portFree` |
| `standins_integration_test.go` | 替身，各按已发布的契约经 mTLS 答：`mdm/org`（法人）、`mdm/currency`（CNY 与 USD）、`erp/inventory` 的 `StockService.Movements`（测试编好号的流水，按纪元与序号答；`reads` 数调用） |

## 加测试

- 新流程：新文件 `<流程>_integration_test.go`，写一个 `func (fx *fixture) <步>()`，在 `TestCostingEndToEnd` 里按依赖排进顺序，在上表加一行。
- 测试不为了通过而改（装配项目规范的铁律）。
