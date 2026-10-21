# 贡献指南（Contributing to IM2Agent）

感谢你考虑为 IM2Agent 贡献代码、文档或意见。本指南说明如何高效、合规地参与。

## 行为准则

所有参与者须遵守 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。请先阅读并认可。

## 项目基线（先读再改）

- **技术文档**：`docs/技术设计基线-v1.0.md` 是架构与协议的权威基线，代码变更须与其一致；不一致时以文档为准并同步修订。
- **协议契约**：`proto/` 中的 Protobuf 是权威协议定义；改动协议必须先改 proto 再改实现。
- **三条不可妥协原则**（README 第七节）：
  1. 付费永远发生在 Agent 预审之后；
  2. 信任边界是代码层 capability 门控（非提示词）；
  3. "不上传原始音频"必须可验证而非声明。
  任何 PR 若削弱这三条，将不予合入。

## 开发流程

1. Fork 本仓库，基于 `main` 新建特性分支：`git checkout -b feat/your-change`。
2. 本地验证必须全绿（Go ≥ 1.21，stdlib-only，离线可编译）：

   ```bash
   make gate        # build + vet + go test -race ./... + 7 服务自测
   ```

3. 提交信息使用清晰的中文或英文描述：`type(scope): 一句话说明`（type: feat/fix/docs/test/refactor/chore）。
4. 发起 Pull Request，说明：动机、改动范围、验证结果、资源/环境闸门（如 PG/Docker/protoc/Flutter 未实跑）。

## 代码约定

- **stdlib-only**：禁止引入外部 Go 模块依赖（当前离线可编译是硬约束）；需要新依赖时先提 issue 讨论。
- 仓储层不 import 具体数据库驱动，驱动由运行时注入（`internal/store`）。
- 所有服务保留 `-selftest` 自测入口；新增机制须同时补单元/端到端测试（`go test -race ./...` 全绿）。
- 生产代码不引入 panic/unwrap 裸断言；错误用 `errors`/哨兵错误传播。

## 提交前检查清单

- [ ] `make gate` 通过（含 `-race`，无数据竞争）
- [ ] 协议/文档与代码一致（`docs/`、`proto/`）
- [ ] 未削弱三条不可妥协原则
- [ ] 新增/修改行为有测试覆盖
- [ ] 未引入外部 Go 依赖
- [ ] 资源闸门（PG/Docker/protoc/Flutter/腾讯云 IM）如实登记，不虚报已通过
