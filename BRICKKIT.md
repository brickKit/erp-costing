# erp/costing

## 组件定位

存货成本：存货价值的唯一权威（D46）。
- **按计价序号计算**：订阅 `erp/inventory` 的流水，按每个“产品 + 法人”的计价序号顺序计价；乱序的先缓存，缺口超过等待时向库存补取。
- **成本层与单价来源**：每条入库流水一个层，单价来源按优先级：政策价（待定价）< `erp/purchase` 的收货计价依据（暂估）< `erp/finance` 的票据单价确认（最终）。
- **调整**：来源替换时按差额发布调整的成本事实，在库部分调库存价值、已出库部分计入销售成本差异；不改写原事实。
- **出库与采购退货**：移动平均或先进先出（按法人）；采购退货按原入库层的当前单价。
- **成本事实**：按 `erp/finance` 的共享会计事实契约发布，财务按法人的科目映射过账。

本版没有：标准成本与定期盘存、关账的封存、票据单价调整（折让与补差票）、到岸成本与生产成本、期初成本层导入、成本重算、销售退货的成本冲回、成本子账与总账的对账。

## 部署前准备

- 数据库：PostgreSQL 16 及以上；schema `erp_costing`，属主角色 `erp_costing_owner`（只供迁移）与运行角色 `erp_costing_rw`（只有读写数据的权限），名字来自装配项目的注册表。三者由部署方在第一次启动前建好；表由本组件的迁移建，迁移里不建角色、不授权。
- 消息系统（`BUS_URL`）与实现实例（`IMPLEMENTATION_INSTANCE`）：成本事实以增量事实发布到流 `erp.costing`（4 个分区）；订阅 `erp.inventory` 的流水、`erp.purchase` 的收货计价依据与撤回、`erp.finance` 的票据单价确认与冲回。装了 `erp/finance` 时，它的配置键 `FINANCE_FACT_SOURCES` 要列出 `erp.costing` 的 `erp.costing.accounting_fact.v1`，法人的科目映射要有成本的六条规则（`erp/finance` 3.0.4 起的目录），否则成本事实等着不过账。
- 部署的服务证书（`SERVICE_TLS_*`）；身份与授权（`IAM_*`、`AUTHZ_GRPC_URL`、`DEPLOYMENT_ID`）；审计（`AUDIT_GRPC_URL`）。
- 后台处理为主：适合外壳。

## 依赖说明

- 必需：数据库、消息系统、身份成员、授权成员、`mdm/org`、`mdm/currency`、`erp/inventory`（流水与按序号补取，3.0.39 起）。
- 订阅、不依赖（本组件不调用它们）：`erp/purchase` 的收货计价依据（没装时入库按政策价）；`erp/finance` 的票据单价确认（没装时暂估不被调整，成本事实也没人过账、只供查询）。可选的审计组件。
- 库存不可达时：缺口补取等下一个周期再试，后面的流水不计价；已有数据照常读取。

## 配置指南

全部键见 `component.yaml` 的 `configSchema`（由 SDK 生成，测试核对）。

| 键 | 说明 |
| --- | --- |
| `VALUATION_GAP_WAIT` | 计价序号的缺口等待（时长，例 `5m`）；缺省 5 分钟，之后向库存补取（协议 05b 的起始值） |
| `VALUATION_BASIS_WAIT` | 采购收货的流水等它的收货计价依据的时长；缺省 5 分钟，之后按政策价入层（待定价） |
| `DEPLOYMENT_ID`、`IAM_ISSUER`、`IAM_JWKS_URL`、`AUTHZ_GRPC_URL`、`SERVICE_TLS_CERT_FILE`、`SERVICE_TLS_KEY_FILE`、`SERVICE_TLS_CA_FILE`（必填） | 受保护的用户面与 mTLS 的系统面要用；部署身份、身份与授权的地址在外壳里是进程级的键 |
| `PG_HOST`、`PG_DATABASE`、`PG_USER`、`PG_PASSWORD`、`PG_SCHEMA`（必填） | 自己的数据库，密码是密钥；迁移另用 `PG_OWNER_USER`、`PG_OWNER_PASSWORD` |
| `BUS_URL`（必填，密钥）、`IMPLEMENTATION_INSTANCE`（必填） | 消息系统；实现实例是安装时生成的随机身份 |
| `AUDIT_GRPC_URL` | 审计组件的系统面；缺时审计记录留在本地，按协议 03 处理 |

依赖的地址由 BrickKit 按依赖注入（`MDM_ORG_GRPC_ENDPOINT`、`MDM_CURRENCY_GRPC_ENDPOINT`、`ERP_INVENTORY_GRPC_ENDPOINT`），不是配置键。

## 契约索引

- **系统面**（本仓库 `contract/proto`，模块 `github.com/brickKit/erp-costing/contract/v3`，标签 `contract/v3.…`）：`be.erp.costing.v3.FactService/Read`（成本事实按身份读回；任一组件，操作 `erp.costing.system.read`）；`be.erp.costing.v3.LayerService/ReturnLimit`（采购退货的可退数量；只许 `erp/purchase`）。
- **用户面**：`contract/openapi.yaml`。操作 `erp.costing.policy.read`、`.manage`，`erp.costing.valuation.read`（计价进度与估值、成本层、成本事实）；都带法人维度。
- **事件**（流 `erp.costing`，schema 在 `contract/events/`）：`erp.costing.accounting_fact.v1`（增量事实，按 `fact_id` 去重；形状是 `erp/finance` 的共享契约 `accounting_fact.v1`）。

## 外壳声明

不是外壳。可以被 Go 的核心外壳（`be/go-core`）承载。
