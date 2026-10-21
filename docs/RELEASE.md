# 版本发布 SOP（im2agent）

> 从版本号决定到五方核验完成的全过程。
> **角色边界**：版本号与发布时间由用户决定；执行者负责门禁、打包、推送、核验；Stable 批准只能由用户操作。
> **原则**：门禁红绝不打 tag；先发布、后部署；每个版本留证据。

---

## 0. 发布前条件（缺一不可）

- [ ] 用户已明确版本号（稳定版 `vX.Y.Z`；预发布必须 `vX.Y.Z-beta.N`）
- [ ] 工作区没有未完成的半成品改动
- [ ] 已阅读本次要发布的 CHANGELOG 条目（newest-first）
- [ ] Go 环境就绪：`go version` ≥ 1.27
- [ ] npm 环境就绪（如发 npm 包）：`npm whoami` 返回账号名

---

## 1. 版本 bump（4 处）

| 文件 | 改什么 |
|---|---|
| `cmd/demo/main.go` | `/healthz` 返回的 `version` 字段 |
| `client/app/lib/main.dart` | `const appVersion = '...'` |
| `packages/core/package.json` | `version` 字段 |
| `README.md` | 顶部 version 徽章 |

---

## 2. CHANGELOG

新条目放在最上面（newest-first）。

格式：

```markdown
## [vX.Y.Z] — YYYY-MM-DD

### Release
- 改了什么

### Not Included
- 明确不包含项（防止误解）

### Known Limits
- 已知限制

### Verification
- 门禁数字：go build / vet / test 通过；冒烟 curl 返回 version
```

`packages/core/package.json` 的版本必须等于最新条目版本。

---

## 3. 打包与门禁

### 3.1 Go 主线

```bash
export PATH="$HOME/sdk/go/bin:$PATH"
make build           # 编译
make vet             # 静态检查
make gotest          # go test -race ./...
make build-linux     # 交叉编译静态 ELF 到 dist/
```

通过标准：build / vet / test 全绿，无 `-race` 报告。

### 3.2 Node 主线

```bash
cd packages/core
npm install
npm test --if-present
```

### 3.3 冒烟

```bash
# 起 demo
go build -o /tmp/im2agent-demo ./cmd/demo
/tmp/im2agent-demo -addr 127.0.0.1:PORT &
sleep 2

# 核验版本号
curl -s http://127.0.0.1:PORT/healthz
# 必须返回 {"app":"im2agent-demo","status":"ok","version":"vX.Y.Z"}

kill %1
```

### 3.4 红灯常见原因

| 失败项 | 原因 | 修法 |
|---|---|---|
| `go build` 失败 | 语法错误 / 未使用 import | 修代码 |
| `go vet` 警告 | 字段未使用 / 可疑断言 | 修代码，不得 `// nolint` 绕过 |
| `go test -race` | 并发竞态 | 定位 race 并修 |
| 冒烟版本号不对 | bump 漏改某处 | 回到第 1 步补齐 |

**门禁不绿：停止，不 commit，不打 tag。**

---

## 4. 提交与打 tag

### 4.1 commit

```bash
# 用 GitHub contents API push（git https 直连不通时）
# commit 消息格式：
#   release: vX.Y.Z — <一句话主题>
#   正文列变更与验证数字
```

### 4.2 打 tag

push 成功后，用 GitHub API 建 tag：

```bash
POST /repos/TwinsEarth/im2agent/git/refs
{
  "ref": "refs/tags/vX.Y.Z",
  "sha": "<COMMIT_SHA>"
}
```

### 4.3 轮询 CI

```bash
GET /repos/TwinsEarth/im2agent/actions/runs?per_page=8
# 找 name === 'CI' 且 head_sha 匹配
# 等 status === 'completed' 且 conclusion === 'success'
```

---

## 5. Stable 审批（只能用户做）

```
GitHub → Releases → Draft a new release
  - Choose a tag: vX.Y.Z
  - Release title: vX.Y.Z
  - Description: 从 CHANGELOG 复制条目
  - Attach binaries: dist/im2agent-linux-amd64 + SHA256SUMS
  - Publish release
```

---

## 6. 五方核验（全部通过才算发布完成）

| # | 渠道 | 核验方法 | 通过标准 |
|---|---|---|---|
| 1 | **npm** | `npm view im2agent-core version` | latest = X.Y.Z |
| 2 | **GitHub Release** | GET `/releases/tags/vX.Y.Z` | `prerelease=false`；资产含 tgz + SHA256SUMS |
| 3 | **资产校验** | 下载 tgz 实算 sha256 | 与 SHA256SUMS 一致 |
| 4 | **Packages** | GitHub Packages 页 | 包版本 = X.Y.Z |
| 5 | **CI** | Actions 页 | 该 commit 的 CI run success |
| 6 | **Compat** | 本地冒烟 | `/healthz` 返回 version = X.Y.Z；客户端版本号一致 |

---

## 7. 生产部署（如包含 relay 变更）

```bash
# SSH 到服务器
cd /opt/im2agent
rm -rf app.bak && cp -a app app.bak

# 上传官方 release 二进制
systemctl stop im2agent
cp /tmp/im2agent-linux-amd64 /opt/im2agent/im2agent
chmod +x /opt/im2agent/im2agent
systemctl start im2agent

# 线上复核
curl -s http://127.0.0.1:8080/healthz
# 必须返回新版本号

systemctl status im2agent --no-pager
```

### 回滚

```bash
systemctl stop im2agent
mv /opt/im2agent/app /opt/im2agent/app.1
mv /opt/im2agent/app.bak /opt/im2agent/app
systemctl start im2agent
```

---

## 8. 收尾

- [ ] 证据入库：`docs/acceptance/vX.Y.Z.md`（冒烟输出、五方核验截图/数字）
- [ ] CHANGELOG 验证数字与实际门禁输出一致
- [ ] 各机器部署（Linux 服务器 / Mac mini / Windows）在发布说明中标注版本
- [ ] 若本版本被后续版本纠正，写明代替关系（如 v0.4.0 → v0.4.1）

---

## 9. 本仓库踩过的坑（防复发）

| 坑 | 症状 | 修法 |
|---|---|---|
| git https 直连不通 | push 超时 | 用 GitHub contents API（GET sha → PUT 更新） |
| demo 监听 `*:port`（IPv6） | `curl localhost` 返回 000 | 用 `curl 127.0.0.1:port` 或 `-addr 127.0.0.1:port` 启动 |
| token 401 | push 401 | 确认 PAT 有效；用 `export TOKEN=...` 后 python 从环境读 |
| npm 403 | publish Forbidden | token 必须是 Publish 权限；scope 包需先在 npmjs 创建 scope |
| npm 传播延迟 | dist-tag 未立即更新 | 等 1-2 分钟重试 |
| `.gitignore` 漏 deploy/.env | 密钥可能入库 | 已加 `deploy/**/.env` 递归忽略 |

---

## 10. 角色与红线（再次强调）

- **用户**：决定版本号、批准 Stable、决定是否部署生产
- **执行者**：门禁、打包、推送、五方核验、部署与线上复核
- **红线**：
  - 门禁红不打 tag
  - 不用 beta 冒充稳定版
  - 不未经用户同意部署生产
  - 不自行决定版本号
