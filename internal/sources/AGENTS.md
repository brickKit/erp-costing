# sources：更高的单价来源进来

成本层的更高来源从别的组件进来（设计 LY02–LY05）：采购的收货计价依据与撤回、财务的票据单价确认与冲回。

## 负责与不负责

- 负责：四种订阅（在消费者的事务里交给 `layers` 记下，排工作项）；工作 `apply-source`：事务外取法人、小数位与即期汇率，把单价折成本位币；再开事务，按范围的 `ID` 顺序锁住相关的范围，交给 `layers` 替换单价、出调整，写回范围的价值；依据到了叫醒等它的范围（LY01）。冲回先于确认到达时什么也不做（确认到达时就是已冲回的）；没有汇率时按业务等待重试（`CURRENCY_NOT_USABLE`）。
- 不负责：怎样替换、调整多少（`layers`、`methods`）；什么时候计价（`sequence`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `sources.go` | `JobApply`；`OnBasis`、`OnWithdrawn`、`OnConfirmed`、`OnReversed`（订阅）；`Apply`（工作）与 `applyBasis`、`applyConfirm`、`applyReverse`；`eachScope`、`lockScope`、`convert` |

## 依赖与被依赖

- 依赖：`core`、`masterdata`（法人、小数位、汇率，事务外）、`policies`（方法）、`layers`、`sequence`（锁范围、写回价值、叫醒）。
- 被依赖：入口 `costing/module.go`（订阅、工作）。

## 表

不拥有表：来源记在 `layers` 的表里。

## 规则在哪

设计 LY01–LY05、AD01；装配项目协议 05b 的“暂估与调整”。

## 测试

`itest/sources_integration_test.go`（`sources`）。
