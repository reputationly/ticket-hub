# 工单中心：后端骨架

本步仅实现账号、用户管理、站点设置与基础设施；没有前端、通知、上游采集或回写功能。需求原文见 `docs/design.md`。

## 启动

需要 Go 1.25+（当前依赖版本）。SQLite 使用纯 Go 驱动，无需 CGO。

通过环境变量设置 `.env.example` 中的配置后执行 `make run`。程序不自动读取 `.env`，没有默认管理员密码；首次启动数据库无用户时必须提供管理员用户名与 12~72 字节密码。`APP_SECRET_KEY` 至少 32 字节，建议使用随机生成的高熵密钥，切勿提交到仓库。

- 默认 `DB_TYPE=sqlite`、`DB_DSN=./data.db`，启动时 AutoMigrate。
- PostgreSQL：`DB_TYPE=postgres`，`DB_DSN=postgres://用户:密码@地址/库名?sslmode=require`。
- `REDIS_URL` 为空使用进程内缓存；多实例部署必须连接同一个 Redis，否则黑名单、锁、计数和广播无法共享。
- `COOKIE_SECURE=true` 用于 HTTPS 生产环境；默认监听 `:8080`。
- 默认会话 24 小时，可在站点设置中更改（1~720 小时），对新登录生效。
- 修改密码、重置密码、编辑或禁用用户后，该用户已有会话失效；至少保留一个启用管理员。
- 内存版缓存重启会丢失退出黑名单和限次记录；生产 Redis 黑名单同样需配置持久化，避免退出的旧 JWT 在缓存丢失后重新生效。

## 接口约定

统一响应：`{"code":"ok","message":"成功","data":...}`；失败使用对应 HTTP 状态码及错误 `code`，不暴露数据库细节。

| 方法、路径 | 说明 |
| --- | --- |
| `GET /healthz` | 检查数据库和缓存 |
| `POST /api/auth/login` | `{username,password}`，返回用户并设置 HttpOnly / SameSite=Lax Cookie，不返回 JWT 正文 |
| `POST /api/auth/logout` | 拉黑当前 jti，清除 Cookie |
| `GET /api/auth/me` | 当前用户 |
| `PUT /api/auth/password` | `{old_password,new_password}`，成功后需重新登录 |
| `GET /api/admin/users` | 分页与 `username` 精确、`role`、`enabled` 筛选 |
| `POST /api/admin/users` | `{username,password,display_name,role,phone,email,enabled}`，enabled 默认 true |
| `GET /api/admin/users/:id` | 用户详情 |
| `PUT /api/admin/users/:id` | 用户资料替换，必填 username/role；enabled 省略时保留；不接受密码修改 |
| `DELETE /api/admin/users/:id` | 删除用户 |
| `POST /api/admin/users/:id/reset-password` | `{password}` |
| `PUT /api/admin/users/:id/disable` | 禁用；重新启用使用用户更新接口 |
| `GET /api/admin/settings` | 默认值与缓存读取 |
| `PUT /api/admin/settings` | 整体更新 `{site_name,logo_url,public_url,session_hours}`，写后失效缓存 |
| `GET /api/admin/audit-logs` | 审计列表，倒序分页 |

分页参数 `page=1&page_size=20`，每页最大 100。管理写操作与审计记录处于同一数据库事务，审计只记录操作、对象、操作者和来源 IP，不记录密码。

同用户名登录失败 5 次后，在首次失败起 15 分钟窗口内禁止继续尝试。登录与会话验证在缓存故障时拒绝放行。Cookie 只接受同源请求，不启用跨域；写请求检查 Origin 和 Sec-Fetch-Site。反向代理不信任客户端伪造的 X-Forwarded-For，代理需保留正确 Host。

## 静态资源

`web/dist` 通过 embed 打包。未来构建前端后，非 `/api` 的 GET/HEAD 请求找不到资源时回退 `index.html`。本步只有占位文本，没有 index.html，因此未构建页面返回 404 JSON，接口路径绝不回退前端。

## 缓存与加密

`internal/cache.Cache` 包括 Get/Set/Del、带所有权令牌的 Lock/Unlock、固定窗口 Incr、Pub/Sub。TTL 必须为正数。锁过期后旧持有者不能删除新锁；锁不自动续租。Pub/Sub 为临时广播、不是持久队列；内存订阅缓冲 64 条，慢消费者可能丢消息，不用于可靠任务传递。

`internal/crypto.New(APP_SECRET_KEY)` 派生独立用途的 AES-256-GCM 密钥，Encrypt/Decrypt 使用随机 nonce 和认证数据。JWT 签名使用另一用途派生密钥。后续渠道配置需调用该工具；本步没有渠道表或密钥 API。

## 校验

```sh
go vet ./... && go test ./...
# PostgreSQL 使用测试专用数据库，AutoMigrate 创建表，测试写入在事务结束时回滚。
TEST_PG_DSN='postgres://...' go test ./internal/api -run TestPostgresHandlers -v
```

handler 测试使用 SQLite 内存数据库和内存缓存，覆盖登录、退出后旧令牌拒绝、限次、权限、密码变更、用户增删改查/禁用/重置、最后管理员保护、设置缓存、审计与错误请求。另有缓存和 AES-GCM 工具测试。
