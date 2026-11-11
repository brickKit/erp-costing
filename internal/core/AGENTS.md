# core：全组件共用的名字与写法（共用）

每个模块都引用、自己什么业务也不做的东西：名字、资源定义、错误原因与校验、审计与事件的写法、配置。改这里会影响所有模块。

## 负责与不负责

- 负责：组件 ID、依赖的 ID、流与事件类型（自己的与订阅的）、资源与操作、维度（法人）、参数（`names.go`）；资源定义 `Definitions()`（全组件一张表）；本组件的错误原因与校验、授权（`Check`）、审计（`Change`）、发件表（`Emit`）、增量事实（`Fact`：发事件与存进 `costing_fact` 同一个事务）、“能力不支持”的路由（`Unsupported`）、可选的请求体（`BindOptional`）、十进制的加减与比较（`rules.go`）；生命周期表的通用写法（`lifecycle.go`）；`Base` 与自己的两个配置键（`base.go`）。
- 不负责：任何业务规则。

## 文件

| 文件 | 内容 |
| --- | --- |
| `base.go` | `Base`：组件的运行时与配置（缺口等待 `GapWait`、计价依据的等待 `BasisWait`），每个模块函数的第一个参数 |
| `names.go` | 常量；`fields`、`Definitions()`（每种资源的维度与字段）。列表能排序、筛选的字段要在这里它的资源下声明，不声明 SDK 就答 `FIELD_NOT_QUERYABLE`；计价进度、成本层、成本事实三种列表共用一个资源 `erp.costing.valuation` |
| `rules.go` | `Err…`（本组件的原因，含没有汇率的 `CURRENCY_NOT_USABLE`）、表单的格式、`EntityFacts`、`BindOptional`、`Check`、`Invalid`、`NotFound`、`Unsupported`、`Change`、`Emit`、`Fact`、`Sum`、`Minus`、`Cmp` |
| `values.go` | 库存流水的读法（`Movement`、`ParseMovement`、种类）、纪元（`EpochForm`、`CmpEpoch`）、计价方法的名字、成本事实的身份（`FactOf`，UUIDv5；触发 `FactValued`、`FactReplaced`、`FactReversed`）、十进制的乘除与舍入（`Mul`、`Quo`、`Amount`、`Plain`）、计价事务里的范围 `Scope` |
| `lifecycle.go` | 生命周期表：`Effect`、`Decision`、`Table`（`Decide`）、`Refused`、造格的 `RefuseCell`、`IgnoreCell`、`ProceedCell`、`MoveCell`；三项核对给各模块的单元测试用：`TableProblems`、`DocumentProblems`（与设计文档的表逐格相同）、`UntestedRules`（写法照 `erp/purchase`，SF30：以后进 SDK） |

## 依赖与被依赖

- 依赖：只有 SDK 与契约。
- 被依赖：全部模块与入口。

## 规则在哪

资源、操作、维度：安全 06、安全 07；事件与发件表：装配项目协议 02；增量事实：协议 05 的共同规则；审计：协议 03；金额：协议 14（不用浮点）；两个等待的起始值：协议 05b 的参数表。

## 测试

入口的单元测试 `costing/contract_test.go`（清单、路由、合并安全）；其余由全部流程测试间接覆盖。
