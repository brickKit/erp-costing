# masterdata：事务外取证（共用）

上游的读取，全部在事务之前（线协议 P4.20）：先取证，再开事务。不可达答“依赖不可用”，不当作“不存在”（协议 07）。

## 负责与不负责

- 负责：到依赖的连接（`Conn`）、法人与本位币（`LegalEntityOf`）、币种的小数位（`CurrencyScale`）、可选依赖装没装（`Deployed`）。
- 不负责：业务判断。

## 文件

| 文件 | 内容 |
| --- | --- |
| `upstream.go` | `Conn`、`LegalEntity`、`LegalEntityOf`、`CurrencyScale`、`Deployed` |

## 依赖与被依赖

- 依赖：`core`；`mdm/org`、`mdm/currency` 的契约。
- 被依赖：随计价的模块加入。

## 规则在哪

装配项目协议 07（不可达不是不存在）、线协议 P4.20（事务外取证）。

## 测试

由流程测试经替身间接覆盖（`itest/standins_integration_test.go`）。
