# facts：成本事实

成本事实的形状与发布（`Publish`，按财务的共享契约 `accounting_fact.v1`），与系统面 `FactService.Read`：别的组件（财务、恢复工具）按事实身份读回本组件发布的成本事实（协议 05 的共同规则：漏收的按身份读回）。

## 负责与不负责

- 负责：成本事实的形状（来源是流水、法人本位币、业务日期即过账日期、维度为空）与发布（为 0 的角色不写、全为 0 不发，CF01、VS02）；按身份读 `costing_fact`，按问的顺序答，不认识的答空类型。
- 不负责：事实的身份（触发方用 `core.FactOf` 推出）；存进 `costing_fact` 与发件表（`core.Fact`，与计价同一个事务）；金额怎样算（`layers`、`issues`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `facts.go` | `Read` |
| `cost.go` | `Accounting`（共享契约的形状）、规则键与角色的名字、`Cost`、`Trigger`、`Publish`（同时记下法人、产品、流水、规则、日期与触发，给列表用） |
| `list.go` | `ListFacts`（路由） |

## 依赖与被依赖

- 依赖：`core`。
- 被依赖：入口的 `services.go`；`layers`、`issues`（发布）。

## 规则在哪

装配项目协议 05 的共同规则；设计 CF01。

## 测试

`itest/sequence_integration_test.go`（入库与出库事实的形状、金额为 0 不发）；`itest/queries_integration_test.go`（列表与触发）。
