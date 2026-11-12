# 工单与通知

## 接口

全部接口需要登录；`/api/admin/*` 仅管理员可用。统一响应沿用 `{code,message,data}`。

| 方法、路径 | 说明 |
| --- | --- |
| `GET /api/tickets` | 全员可查，分页；精确筛选 `kind/status/site`，`keyword` 模糊标题或上游编号，`from/to` 为 RFC3339 创建时间范围 |
| `GET /api/tickets/:id` | `{ticket,events}`，事件按创建顺序排列 |
| `GET /api/notifications` | 当前用户消息分页，`read=true/false` |
| `GET /api/notifications/unread-count` | `{count}` |
| `PUT /api/notifications/:id/read`、`PUT /api/notifications/read-all` | 已读操作，不能访问其他用户消息 |
| `GET /api/notifications/stream` | Cookie 鉴权 SSE，事件名 `notification`；断线后重新拉列表校准 |
| `GET/PUT /api/me/subscription` | GET 返回 `{subscription,available_channels}`，PUT 整体替换订阅 |
| `PUT /api/me/profile` | `{phone,email,feishu_open_id}`，空值清空 |
| `GET/POST /api/admin/channels`、`GET/PUT/DELETE /api/admin/channels/:id` | 渠道管理，列表 `{items}` |
| `POST /api/admin/channels/:id/test` | `{user_id,phone,email,title,content,mentioned_mobile_list,mention_open_ids}`；异步测试，返回 job，结果查 notify-jobs |
| `GET/POST /api/admin/rules`、`GET/PUT/DELETE /api/admin/rules/:id` | 规则 CRUD，列表分页 |
| `POST /api/admin/templates/preview` | `{template,ticket,event}` → `{content}` |
| `GET /api/admin/notify-jobs` | 分页，支持 `status/channel_type/channel_id/user_id/event_id/from/to` |
| `POST /api/admin/notify-jobs/:id/resend` | 重置失败重试次数并入队；正在处理的任务拒绝重发 |
| `POST /api/dev/tickets` | 仅 `DEV_MODE=true` 时注册，且仅管理员可用；请求为工单字段 JSON |

事件类型为 `ticket.created`、`ticket.status_changed`、`ticket.updated`。`ticket.Service.Upsert(ctx, model.Ticket)` 使用 `(kind,upstream_id)` 唯一键，只在 `status/node/assignee/deadline/title` 哈希变化时创建事件；工单与事件同事务提交，再分发通知。投递失败保留未分发事件，后台恢复。截止时间使用 RFC3339 或 null；`raw` 保留原始 JSON。

## 规则与订阅

```json
{"name":"维修待领","enabled":true,"conditions":{"kinds":["repair"],"statuses":["待认领"],"sites":["北京"],"keyword":"服务器","event_types":["ticket.created"]},"actions":{"user_ids":[1],"group_channel_ids":[],"personal_channels":["inapp","email"],"template":"{{.Ticket.Title}} {{.Event.Type}} {{.Link}}"}}
```

条件列表为空表示不限，关键词匹配标题或上游编号。规则与启用的订阅独立匹配并合并，按用户/渠道实例和群实例去重；规则按 ID 递增，同目标首次匹配的模板优先。模板使用 Go `text/template`，可用 `.Ticket/.Event/.Link`，Link 来自站点 `public_url`；空模板使用中文默认模板。模板限制 16KB，渲染结果限制 64KB。

订阅字段：`enabled/kinds/sites/event_types/channels/quiet_start/quiet_end/timezone/urgent_bypass/mention_in_group`。`channels` 只能选择管理员已启用的 `sms_aliyun/email/inapp`（也接受 `sms` 别名）。免打扰使用 `HH:MM`、含开始不含结束、支持跨午夜；相同开始结束表示全天。时区建议设 `Asia/Shanghai`，未填使用服务本地时区。紧急优先级为 `urgent/critical/p0/p1/紧急`。

匹配到用户后始终生成一条站内兜底消息，即使未启用站内渠道或处于免打扰；免打扰期间不生成外部个人消息、不在群里 @。群消息仍发送，只有关注该事件且打开 `mention_in_group` 的非免打扰用户才被 @。短信默认仅新建或紧急工单发送。

## 渠道配置

`access_key_id/access_key_secret/password/webhook/secret` 均 AES-GCM 加密落库，API 回显 `********`；PUT 原样传打码值或省略密钥表示保留，传空字符串表示清空可选密钥。渠道类型不可修改，配置不接受未声明字段。端口为字符串。

| type | config |
| --- | --- |
| `sms_aliyun` | `access_key_id/access_key_secret/sign_name/template_code/template_params`，映射如 `{"name":"title","state":"status"}` |
| `email` | `host/port/username/password/from/tls/html_template`；tls 为 `ssl/starttls/none`，HTML 模板使用 `.Title/.Content/.TicketID` 等 Delivery 字段并自动 HTML 转义 |
| `wecom_bot` | `webhook`，Markdown 正文；手机号 @ 使用补充 text 消息的 `mentioned_mobile_list`（企微 Markdown 不支持此字段），两条请求都计入限额 |
| `feishu_bot` | `webhook/secret`，交互卡片；有 open_id 时 @，无标识时显示名字文本；可选 HMAC-SHA256 签名 |
| `inapp` | `{}`，DB 持久消息与 cache Pub/Sub |

短信映射可用字段：`title/status/kind/site/upstream_id/node/assignee/priority/event/link`。阿里云使用官方 Tea SDK v4；机器人和 SDK 的 HTTP client/base URL 通过 `sender.Options` 注入，仅供本地桩测试，不向 API 暴露任意 endpoint。SMTP 支持三种 TLS 模式，生产不要使用 none 传输凭据。

## 队列与可靠性

Redis 启用时使用 Stream `ticket-hub:notify` 和消费组；未配置时使用内存 channel。DB `notify_jobs` 是持久事实来源，启动及每秒恢复到期 pending/租约超时 processing 任务。处理使用 DB 条件更新认领和 1 分钟租约，重复入队不会并发重复发送。限频采用 cache 固定时间窗口：企微每 Webhook 20/分钟、飞书 5/秒和 100/分钟、短信每手机号 1/分钟；超限保留 pending 并在下一窗口重入队，不消耗失败重试次数。失败按 1 秒、10 秒、60 秒、5 分钟重试 4 次（最多发送 5 次）。

站内按 job_id 幂等。外部消息为至少一次：外部接收成功后进程崩溃或响应丢失仍可能重发，不能保证跨外部服务 exactly-once。测试与手动重发也经过同一队列和频控。SSE/PubSub 不保证离线补发，断线后应重新拉取消息列表与未读数。

SSE 需反向代理关闭缓冲并配置足够长的读取超时；服务禁用全局 WriteTimeout 以支持长连接。多实例必须使用相同数据库、Redis 和 APP_SECRET_KEY。Redis 不可连接时启动失败，不会悄悄切换不共享的内存队列。

## 本地联调

设置 `DEV_MODE=true`，启动后登录管理员，使用同一 Cookie 依次调用：

```text
POST /api/admin/channels
{"name":"站内提醒","type":"inapp","enabled":true,"config":{}}

POST /api/admin/rules
{"name":"所有新工单","enabled":true,"conditions":{"event_types":["ticket.created"]},"actions":{"user_ids":[1],"personal_channels":["inapp"]}}

POST /api/dev/tickets
{"kind":"repair","upstream_id":"DEV-001","title":"测试工单","status":"待认领","site":"北京"}

GET /api/notifications
```

worker 异步处理，消息通常一秒内可见。这里不需要上游登录或任何外部通知凭据。

## 测试

```sh
go vet ./... && go test ./...
TEST_PG_DSN='postgres://...' TEST_REDIS_URL='redis://127.0.0.1:6379/15' go test ./...
TEST_PG_DSN='postgres://...' TEST_REDIS_URL='redis://127.0.0.1:6379/15' go test -v ./... -run 'PG|Postgres|Redis'
```

仅设置变量时运行 PG/Redis 测试；须使用专用测试实例。渠道全部使用本地 httptest/假 SMTP，不发送真实消息。SMTP 测试覆盖 SSL、STARTTLS、none；分发测试覆盖匹配、去重、跨午夜免打扰、限频延迟、四次重试与租约超时恢复。
