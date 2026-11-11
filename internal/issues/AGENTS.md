# issues：出库与退货的成本

离开库存的成本（设计 IS01、IS02、RT01）：出库、采购退货。

## 负责与不负责

- 负责：出库（IS01：移动平均交给 `methods.AverageTake`；先进先出从最早的层的前面取单位，按各单位的单价；取走全部时取走全部价值）；在库不够的部分按政策价、可为负并告警，之后的入库先补平、差额更正销售成本（IS02，`Level`）；采购退货（RT01：先进先出取原层接下来的单位，移动平均按原层的加权单价，层的已出库数量随之加）；系统面 `ReturnLimit`；出库、退货、补平的成本事实。
- 不负责：入库与政策价、层里哪几个单位是什么价（`layers`）；计价顺序（`sequence`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `issues.go` | `Issue`（发 `cost_of_sales`）、`fifo`、`movingAverage`、`leave`（离开范围的价值与超出部分）、`Return`（发 `inventory_return`）、`ReturnLimit`（系统面）、`Level`（补平，发 `cost_of_sales` 或 `cost_of_sales_return`） |

## 依赖与被依赖

- 依赖：`core`、`facts`、`layers`（政策价、取单位）、`methods`、`policies`（`ReturnLimit` 读方法）。
- 被依赖：`sequence`；入口的 `services.go`（`ReturnLimit`）。

## 规则在哪

设计 IS01、IS02、RT01、CF01；装配项目协议 05b 的“计算与缺口”“退货”。

## 测试

`itest/sequence_integration_test.go`（`sequence`：乱序后的移动平均出库、取走全部）；`itest/issues_integration_test.go`（`fifo`：先进先出出库、AD02 的先进先出分法、采购退货与可退数量、在库不够与补平；`returns`：移动平均的采购退货与补平、只许采购问）。
