# sequence：计价序列

按库存给“产品 + 法人”编的计价序号，一条一条计价（设计 VS01–VS05）；计价进度的生命周期表与查询。

## 负责与不负责

- 负责：收库存的流水（按事实身份与序号放进 `movement_in`，第一次见到的范围在流水的纪元从 1 等起）；处理工作 `process-scope`：每步一个事务看下一号——计价（交给 `layers`、`issues`）、等依据、等缺口、停下；事务之间向库存补取缺口（VS03）或把旧纪元补到底后改用新纪元（VS04）；更旧纪元的流水丢弃并告警；计价进度的读取与列表；给 `sources` 锁范围、写回价值、叫醒（`stock.go`、`Wake`）。
- 不负责：每种流水怎样计价（`layers`、`issues`）；计价方法（`policies`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `lifecycle.go` | 计价进度的表（`core.Table`）：状态 `running`、`waiting_gap`、`waiting_basis`、`stopped`，九个命令；与设计的表逐格相同 |
| `lifecycle_test.go` | 单元测试：表的每格完整、与设计逐格相同、每条规则有集成测试提到 |
| `intake.go` | `JobProcess`；`OnMovement`、`keep`（收与补取共用）、`Wake`（`scope.queued`：同一范围只排一个立即的工作项） |
| `stock.go` | `Lock`、`ScopeOf`、`SaveValue`：来源替换在计价序列之外改范围的价值，持同一把锁 |
| `process.go` | `Process`（一次运行至多 500 步，多了排下一次）、`step`、`lock`、`valueNext`、`missing`、`waitFor`（按数据库的时钟排到期的工作项）、`stop`、`save`、`readGap`、`readToEnd`、`readBack` |
| `scopes.go` | `GetScope`、`ListScopes`（路由） |

## 依赖与被依赖

- 依赖：`core`、`masterdata`（法人、小数位、库存的补取，都在事务外）、`policies`、`layers`、`issues`。
- 被依赖：入口 `costing/module.go`（订阅、工作、路由）；`sources`。

## 表

`scope`、`movement_in`、`valued_movement`。

## 规则在哪

设计 VS01–VS05、LY01（等依据）；装配项目协议 05b 的“计价序列”“计算与缺口”，协议 14 的纪元。

## 测试

`lifecycle_test.go`（表、与设计逐格、每条规则有集成测试提到）；`itest/sequence_integration_test.go`（`sequence`、`unknownKind`、`epochs`）。
