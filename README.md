# 工单中心：只读同步与多渠道提醒

面向供应商运维团队的 Go 单体服务。集中同步上游工单、记录变化并分发提醒；**不提供认领、处理、审批、结单或回写上游的能力**。

> 真实 Dora 工单接口尚未授权抓包。当前是可配置登录骨架、同步框架和开发 mock，不能宣称已支持真实 Dora 业务采集。

## 功能

- 中文响应式工作台：桌面侧栏和表格，手机底部导航和卡片；筛选、分页/滚动加载、工单详情、事件时间线、原始 JSON。
- 手机工单首屏使用一行搜索和筛选按钮；详细条件放入底部抽屉。个人资料、密码、订阅、跨午夜免打扰、紧急突破、群 @。
- SSE 实时站内消息、未读角标、已读、可选浏览器通知。
- 管理：站点品牌、用户、IMAP/SMTP、上游账号、采集调度、邮件触发规则、通知渠道、规则与模板预览、发送/同步/审计日志。
- 五类 Sender：阿里云短信、SMTP、企业微信机器人、飞书机器人、站内消息。配置密钥 AES-GCM 加密落库，API 打码。
- 通知队列：匹配规则与订阅、去重、免打扰、限频延迟、失败重试、超时恢复。外部发送为至少一次语义，网络异常下可能重复。
- SQLite（纯 Go，无需 CGO）/PostgreSQL、可选 Redis、HttpOnly Cookie/JWT、登录限次、角色权限、会话失效和管理审计。

## 架构

```text
IMAP 验证码/通知邮件 ─┐
定时调度/手动同步 ────┴→ 上游登录状态机 → Collector → Ticket.Upsert
                                                       │
                                             工单事件 → 规则/订阅
                                                       │
                                           notify_jobs + 可靠队列
                                                       │
                                        worker → 五类 Sender → SSE

浏览器 → React 按路由懒加载 → Gin API / Go embed 静态资源
                             ├─ GORM → SQLite 或 PostgreSQL
                             └─ cache/queue → 内存或 Redis
```

前端独立路由动态导入、第三方依赖分块，不提高构建告警阈值。Go 二进制内嵌前端，无需独立静态服务器。Redis 实现缓存、锁、计数、Pub/Sub 和 Stream 消费组；未配置时使用单进程内存实现。多实例需共享 PostgreSQL 和 Redis。

## 快速开始：本地 SQLite

需要 Go 1.25.1+、Node.js 24、pnpm 12.16.1（版本见 `web/package.json`）。

```sh
cp .env.example .env
# 编辑 .env：填写 APP_SECRET_KEY、ADMIN_USERNAME、ADMIN_PASSWORD。
# 建议 APP_SECRET_KEY 用 openssl rand -hex 32，密码用 openssl rand -hex 24。
chmod 600 .env
set -a
. ./.env
set +a
pnpm --dir web install --frozen-lockfile
make build
./bin/ticket-hub
```

打开 `http://127.0.0.1:8080`，用配置的管理员登录。默认 SQLite 在当前目录 `data.db`。程序**不自动加载 `.env`**，本地示例显式加载；Compose 自动读取同目录 `.env`。首次无用户才创建管理员，修改环境变量不会重置已有密码。

开发可运行 `make run`，另一个终端 `pnpm --dir web dev`。Vite `/api` 代理到 `127.0.0.1:8080` 并保留 Host，通过后端同源校验；本地 HTTP 保持 `COOKIE_SECURE=false`。

## 快速开始：Docker Compose

需要 Docker Engine 和 Compose v2+；宿主机无需安装 Go/Node，所有基础镜像使用官方 Docker Hub 名称。

```sh
cp .env.example .env
# 编辑 .env，APP_SECRET_KEY、ADMIN_PASSWORD、POSTGRES_PASSWORD 必填。
# PG 密码建议 openssl rand -hex 24，避免连接 URL 的特殊字符。
chmod 600 .env
docker compose up -d --build
docker compose ps
curl --fail http://127.0.0.1:8080/healthz
```

默认启动 app + postgres:16 + redis:7，app 等待 PG/Redis 健康。仅 app 端口映射到 `127.0.0.1:8080`，数据库和 Redis 不暴露宿主端口。PG、Redis AOF、应用目录均使用命名卷。

镜像为 Node 前端构建 → Go `CGO_ENABLED=0` 编译 → Alpine 运行三个阶段，运行 UID/GID 为 10001，包含 CA 证书和时区数据。Compose 根文件系统只读，`/tmp` 使用 tmpfs，应用数据目录可写。app 的 `/healthz` 同时检查数据库和缓存。

主 Compose 固定使用容器内 PG/Redis 地址，忽略 `.env` 中的本地 `DB_TYPE/DB_DSN/REDIS_URL`。`APP_PORT` 修改宿主端口，`APP_BIND` 默认本机；远程生产访问请通过 HTTPS 反向代理、保留 Host、支持 SSE 长连接，并设置 `COOKIE_SECURE=true`，不要直接暴露数据库。

```sh
docker compose logs -f app
docker compose down
# down 保留命名卷；down -v 会删除数据库，慎用。
```

### SQLite 精简部署

独立的 `compose.sqlite.yml` 只启动 app，**不要叠加主 Compose 文件**：

```sh
# .env 仅需 APP_SECRET_KEY、ADMIN_USERNAME、ADMIN_PASSWORD，不需要 PG 密码。
docker compose -f compose.sqlite.yml -p ticket-hub-sqlite up -d --build
curl --fail http://127.0.0.1:8080/healthz
docker compose -f compose.sqlite.yml -p ticket-hub-sqlite down
```

SQLite 保存在 `sqlite-data` 命名卷，使用内存缓存/队列，适合单实例小规模部署。和主部署同时运行时设置不同的 `APP_PORT`；不能让多个实例共享 SQLite 文件或依赖内存锁。

## 配置项

完整示例见 `.env.example`。不要提交 `.env`、密钥、Cookie 或邮箱授权码。

| 变量 | 默认值 / 说明 |
| --- | --- |
| `LISTEN_ADDR` | 本地 `:8080`；容器固定监听 8080 |
| `DB_TYPE` | 本地 `sqlite` 或 `postgres` |
| `DB_DSN` | 本地默认 `./data.db`；PG 使用 `postgres://用户:密码@主机:端口/库?sslmode=require` |
| `REDIS_URL` | 空为内存；多实例需同一 Redis，生产应持久化 |
| `APP_SECRET_KEY` | 必填，至少 32 字节高熵密钥，派生 AES/JWT 密钥；必须安全备份，更换影响会话和加密数据 |
| `ADMIN_USERNAME` | 首次启动管理员名 |
| `ADMIN_PASSWORD` | 首次必填，12~72 字节；已有用户时忽略 |
| `COOKIE_SECURE` | 默认 false，HTTPS 生产必须 true |
| `DEV_MODE` | 默认 false；true 开放管理员假工单注入、mock 上游/采集器 |
| `TEST_PG_DSN` / `TEST_REDIS_URL` | 专用集成测试连接，不得指向生产 |
| `POSTGRES_USER` / `POSTGRES_DB` | Compose 默认 `ticket` / `ticket_hub` |
| `POSTGRES_PASSWORD` | 主 Compose 必填，建议随机十六进制密码 |
| `APP_BIND` / `APP_PORT` | Compose 默认 `127.0.0.1` / `8080` |

站点名、Logo、对外访问地址和会话时长在管理后台设置。会话默认 24 小时，修改时长对新登录生效；重置密码、管理更新/禁用账号会使旧会话失效。登录同用户名 5 次失败/15 分钟限次，至少保留一个启用管理员。

Cookie 为 HttpOnly/SameSite=Lax，写请求检查 Origin/Sec-Fetch-Site，不支持跨域。反向代理需保留 Host。退出黑名单依赖缓存，内存重启会丢失；生产 Redis 需持久化。Pub/Sub 不是可靠任务队列，SSE 重连后重新查询通知列表补齐消息。

## 上游对接现状

预注册十四类：故障维修、服务器下架、服务器改配、物流、巡检、机柜操作、协查、入室、事故复盘、机柜上下电、安全、事件、维护、变更。

- Dora Authenticator 具备可配置发码/验码骨架、CSRF、加密 Cookie Jar、保活和状态机，请求细节仍待真实抓包确认。
- **真实 Dora Collector 未实现**，明确返回“未实现，待抓包”；不会猜测或调用业务接口。
- 已有 IMAP IDLE/轮询、MIME 解析、验证码等待、邮件触发、登录锁、三次失败告警、同步去抖/退避和日志。
- 开发联调：`DEV_MODE=true`，创建 mock 上游并登录，启用多个 kind 的 mock 采集器；建立 inapp 渠道和指向管理员的规则即可观察工单和消息。不要启用真实外部渠道。
- 真实接入前需要授权抓取列表/分页/筛选/详情及会话保活请求，核对权限、状态枚举和字段，再实现只读 Collector。

## 目录结构

```text
cmd/server/                服务入口和生命周期
internal/api/              鉴权、工单、管理、SSE API
internal/auth,crypto/      JWT、密码、AES-GCM
internal/cache,queue/      内存/Redis、Stream 队列
internal/config,db,model/  配置、迁移、模型
internal/ticket,notifier/  Upsert、事件、通知分发
internal/sender/           五类渠道
internal/mailwatch/        IMAP、MIME、验证码、邮件触发
internal/upstream/         登录状态机与 Authenticator
internal/collector,syncer/ 注册表、采集接口、调度
web/src/                   独立路由页面、管理表单、响应式样式
web/scripts/               Chromium 截图与联调
web/dist/                  Vite 产物，Go embed，不提交产物
docs/                      设计和模块说明
.github/workflows/ci.yml   Go/前端校验、Docker 构建（不推送）
```

## 测试与 CI

```sh
go vet ./... && go test ./...
pnpm --dir web lint && pnpm --dir web build
# 使用专用测试库和 Redis：
TEST_PG_DSN='postgres://...' TEST_REDIS_URL='redis://...' go test -count=1 -v ./...
go install github.com/rhysd/actionlint/cmd/actionlint@latest
actionlint .github/workflows/ci.yml
```

GitHub Actions 在 push/PR 时运行 Go vet、完整测试（真实 PG/Redis services）、前端 lint/TypeScript/build；通过后构建 Docker 镜像，**不推送**。渠道测试使用本地 HTTP/SMTP/IMAP 桩，不需要真实外部凭据。

Chromium 截图：准备 mock 工单和站内规则，在环境中提供 `ADMIN_PASSWORD`，执行 `pnpm --dir web exec playwright install chromium`、`pnpm --dir web screenshots`，默认输出 `~/ticket-hub-validation/screens/`，不进仓库。

API 统一返回 `{code,message,data}`，分页 `page/page_size`（最多 100）。详见 [通知说明](docs/notifications.md)、[上游说明](docs/upstream.md)、[前端说明](docs/frontend.md)、[原始设计](docs/design.md)。
