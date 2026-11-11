# layers：成本层与单价来源

入库流水的成本层与它的单价来源（设计 LY01–LY05）：收货计价依据、政策价、（随 P3c）票据单价确认与冲回、依据撤回。

## 负责与不负责

- 负责：登记采购的收货计价依据（按收货行）；入库建层（LY01：有依据按依据，采购入库没依据时等，等到期或别的来源单据按政策价）；政策价（`PolicyPrice`：最近一层的单价、没有层时当前移动平均、都没有是 0 并告警）；入库的成本事实。
- 不负责：什么时候计价、等多久（`sequence`）；出库与退货（`issues`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `layers.go` | 来源 `SourcePolicy`、`SourceBasis`、`SourceInvoice`；`RegisterBasis`（订阅的一半，另一半在 `sequence.OnBasis` 叫醒等依据的范围）；`PolicyPrice`；`Receive`（入层，范围的数量与价值随之变，发 `inventory_receipt`） |

## 依赖与被依赖

- 依赖：`core`、`facts`。
- 被依赖：`issues`（政策价）、`sequence`。

## 表

`cost_layer`、`layer_source`、`basis`。

## 规则在哪

设计 LY01–LY05、CF01；装配项目协议 05b 的“暂估与调整”。

## 测试

`itest/sequence_integration_test.go`（`sequence`：依据入层、等依据、政策价、价格为 0 不发事实）。
