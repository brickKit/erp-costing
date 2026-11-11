# erp-costing

组件 `erp/costing`：存货成本。按 `erp/inventory` 的计价序号计算每条入库与出库的成本（移动平均或先进先出），成本层记下单价来自政策价、采购的收货计价依据还是财务的票据单价确认，来源替换时按差额出调整，成本按 `erp/finance` 的共享会计事实契约发给财务过账。使用说明见 `BRICKKIT.md`，修改说明见 `AGENTS.md`，设计见 `docs/design.md`。

按终局设计从零开始（D59），版本从 `3.0.0` 起（A110）。

## 在项目里使用

在装配项目里 `brickkit add erp/costing@<版本>`，再按 `BRICKKIT.md` 的“部署前准备”配置。

## 文档

| 问题 | 看哪里 |
| --- | --- |
| 它做什么、怎样部署与配置 | `BRICKKIT.md` |
| 为什么这样设计：术语、数据、规则、未决问题 | `docs/design.md` |
| 怎样改它 | `AGENTS.md` |
| 契约 | `contract/README.md` |

## 开发

见 `AGENTS.md` 的“构建与测试”。
