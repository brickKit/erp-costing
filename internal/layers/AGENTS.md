# layers：成本层与单价来源

入库流水的成本层与它的单价来源（设计 LY01–LY05、AD01）：收货计价依据、政策价、票据单价确认与冲回、依据撤回，来源替换出的调整。层里的每个单位取覆盖它的最高来源：票据按确认到达的先后占没开票的单位（从层的前面起，冲回留下的空位先补），其余单位取底价（依据，没有时政策价）。

## 负责与不负责

- 负责：记下四种来源（依据、撤回、确认与匹配、冲回；先到的冲回也记）；入库建层（LY01：有折好的、没撤回的依据按依据，采购入库没依据时等，等到期或别的来源单据按政策价；为它挂起的确认按票据价入层，不出调整）；政策价（`PolicyPrice`：最近一层的单价、没有层时当前移动平均、都没有是 0 并告警）；替换（`ApplyBasis`、`ApplyConfirm`、`ApplyReverse`）与调整（AD01：按 `methods.Split` 分，向上向下两条规则、金额都为正，库存的部分加进范围的价值）；撤回只告警（LY05）；入库与调整的成本事实。
- 不负责：什么时候计价、等多久（`sequence`）；来源从哪进来、折成本位币、锁哪些范围（`sources`）；出库与退货（`issues`）；方法之间的不同（`methods`）。

## 文件

| 文件 | 内容 |
| --- | --- |
| `layers.go` | 来源 `SourcePolicy`、`SourceBasis`、`SourceInvoice`；`RegisterBasis`；`PolicyPrice`；`Receive`（入层，范围的数量与价值随之变，发 `inventory_receipt`） |
| `units.go` | 层与它有效的来源（`load`）：现在的最高来源 `state`、没开票的单位 `uncovered`、`take`、`value`、`addInvoice`、`setBase`、`save`（加权单价与最高来源） |
| `sources.go` | `RegisterWithdrawal`（LY05）、`RegisterConfirmation`、`RegisterReversal`、`BasisByLine`、`ConvertBasis`、`LockConfirmation`、`ConvertConfirmation`、`MatchesOf`、`MarkReversed`、`ReversalOf`；`ApplyBasis`、`ApplyConfirm`、`ApplyReverse`；`applyPending`（新层接挂起的确认）；`adjust`（AD01） |
| `take.go` | 单位从层的前面离开：`rangeValue`、`takeFront`（记 `issue_layer`）、`TakeOldest`（先进先出出库）、`OriginOf`、`TakeFromLayer`（先进先出的退货与补平）、`MarkGone`（移动平均的退货） |
| `lifecycle.go` | 成本层单价来源的表（`core.Table`）：状态 `policy`、`basis`、`invoice`，命令 `basis`、`withdraw`、`confirm`、`reverse` |
| `lifecycle_test.go` | 单元测试：表的每格完整、与设计逐格相同、每条规则有集成测试提到 |

## 依赖与被依赖

- 依赖：`core`、`facts`、`methods`。
- 被依赖：`issues`（政策价、取单位）、`sequence`（入层）、`sources`（来源）。

## 表

`cost_layer`、`layer_source`、`issue_layer`、`basis`、`basis_withdrawal`、`confirmation`、`confirmation_match`、`reversal`。

## 规则在哪

设计 LY01–LY05、AD01、CF01；装配项目协议 05b 的“暂估与调整”。

## 测试

`lifecycle_test.go`；`itest/sequence_integration_test.go`（`sequence`：依据入层、等依据、政策价、价格为 0 不发事实）；`itest/sources_integration_test.go`（`sources`：依据替换政策价、外币折算、票据确认与冲回、先到的冲回、挂起的确认、开票后的依据、撤回）。
