# erp-costing 的契约

`erp/costing` 发布的契约包：系统面的 protobuf、事件载荷的 JSON Schema、用户面的 OpenAPI。放在组件仓库的 `contract/` 子目录里单独发布（标签 `contract/v3.…`）。

依据：装配项目 [be-assembly-standard](https://github.com/brickKit/be-assembly-standard) 的协议 05b（存货成本：计价序列、暂估与调整、退货、输出）、05（会计事实契约、不可遗漏的增量事实）、14（金额、纪元、列表）；组件的 `docs/design.md`。

## 内容

| 位置 | 是什么 |
| --- | --- |
| `proto/be/erp/costing/v3/costing.proto` | `FactService.Read`（按事实身份读回本组件的成本事实）；`LayerService.ReturnLimit`（采购退货前问可退数量：先进先出下不超过原入库层的在库数量，只许 `erp/purchase`） |
| `gen/go/be/erp/costing/v3/` | Go 代码，包 `costingv3`；模块 `github.com/brickKit/erp-costing/contract/v3` |
| `python/` | Python 包 `contract-erp-costing`，模块 `be.erp.costing.v3` |
| `events/` | 流 `erp.costing`：成本事实 `erp.costing.accounting_fact.v1`（增量事实；schema 只是指向 `erp/finance` 共享契约 `$id` 的 `$ref`，不复制，Go 的测试从财务契约的包 `events` 加载它） |
| `openapi.yaml` | 用户面：计价政策、计价进度与估值、成本层、成本事实；每个路由的操作写在 `x-operation` |

## 要点

- **存货价值只有一个权威**（D46）：按库存的计价序号计算，库存只管数量，财务按成本事实过账、不另算。
- **来源不写科目**：成本事实带过账规则键与角色金额，财务按法人的映射出分录。
- **金额**是十进制字符串，法人本位币；单价不舍入。
