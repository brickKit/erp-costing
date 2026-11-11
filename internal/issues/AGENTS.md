# issues：出库与退货的成本

离开库存的成本（设计 IS01、IS02、RT01）：出库、采购退货。

## 负责与不负责

- 负责：移动平均出库（出库数量 ÷ 在库数量 × 在库价值，取走全部时取走全部价值）；在库不够的部分按政策价、在库可为负并告警（IS02）；出库的成本事实。随 P3e：先进先出逐层出库、采购退货。
- 不负责：入库与政策价（`layers`）；计价顺序（`sequence`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `issues.go` | `Issue`（发 `cost_of_sales`）、`Return`（P3e 前答“本版本不计价”）、`movingAverage`（取数交给 `methods.AverageTake`，超出在库时告警） |

## 依赖与被依赖

- 依赖：`core`、`facts`、`layers`（政策价）、`methods`。
- 被依赖：`sequence`。

## 规则在哪

设计 IS01、IS02、RT01、CF01；装配项目协议 05b 的“计算与缺口”“退货”。

## 测试

`itest/sequence_integration_test.go`（`sequence`：乱序后的移动平均出库、取走全部）。
