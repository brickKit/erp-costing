# policies：计价政策

法人的存货计价方法（设计 PO01）：移动平均（缺省）或先进先出，按版本修改；法人已有计价过的流水时不能再改（`POLICY_IN_USE`）。

## 负责与不负责

- 负责：读取与修改的路由；计价时取方法（`MethodOf`：写缺省行并持 `FOR SHARE` 到计价事务结束，修改的 `FOR UPDATE` 等它，之后就看到“已使用”）。
- 不负责：按方法计价（`layers`、`issues`）；计价进度（`sequence`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `policies.go` | `MovingAverage`、`FIFO`；`Policy`；`GetPolicy`、`SetPolicy`（路由）；`MethodOf`（计价事务里用） |

## 依赖与被依赖

- 依赖：`core`、`masterdata`（法人是否存在，事务外）。
- 被依赖：`sequence`、`issues`、入口 `costing/module.go`。

## 表

`policy`（版本 0 的行是没人设过的缺省）。

## 规则在哪

设计 PO01。

## 测试

`itest/policy_integration_test.go`（`policies`、`policyInUse`）。
