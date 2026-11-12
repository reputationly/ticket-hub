# 邮件监听与上游同步框架

本阶段不调用任何猜测的 Dora 业务 API，不认领、审批、结单或回写上游。真实采集器返回 `未实现，待抓包`，配置页可从 `last_error` 看到原因。所有预注册采集器默认禁用。

## 配置与管理接口

以下接口均仅管理员可用；使用已有登录 Cookie、统一 JSON 响应格式。列表中的 upstream、mail-rules、sync-logs 支持 `page/page_size`，collectors 与 mail-accounts 返回 `data.items`。PUT 为整体配置替换，需提交完整配置。

| 路径 | 方法及用途 |
| --- | --- |
| `/api/admin/mail-accounts` | GET / POST，邮箱列表、新建 |
| `/api/admin/mail-accounts/:id` | GET / PUT / DELETE |
| `/api/admin/mail-accounts/:id/test` | POST，IMAP 登录并 SELECT INBOX，返回 `inbox_count` |
| `/api/admin/upstream` | GET / POST，上游账号列表、新建 |
| `/api/admin/upstream/:id` | GET / PUT / DELETE |
| `/api/admin/upstream/:id/status` | GET，登录状态、时间、失败次数；无 Cookie 或验证码 |
| `/api/admin/upstream/:id/login` | POST，发码→等待邮件→验码→保活；请求最多等待十分钟 |
| `/api/admin/upstream/:id/manual-code` | POST `{ "code":"验证码" }`，等待流程中的手工兜底 |
| `/api/admin/collectors` | GET，所有采集器与最近运行状态 |
| `/api/admin/collectors/:kind` | PUT，更新配置 |
| `/api/admin/collectors/:kind/sync` | POST，异步排队立即同步；`queued:true` 表示已接收或去抖合并，不代表同步成功 |
| `/api/admin/mail-rules` | GET / POST，邮件触发规则 |
| `/api/admin/mail-rules/:id` | GET / PUT / DELETE |
| `/api/admin/sync-logs` | GET，按 `kind`、`status=success/error`、`from/to` 筛选 |

邮箱示例（仅示例域名，切勿填写未经授权的账号）：

```json
{"name":"上游收件箱","host":"imap.example.com","port":993,"tls":"ssl","username":"tickets@example.com","password":"授权码","smtp_host":"smtp.example.com","smtp_port":465,"smtp_tls":"ssl","enabled":true}
```

IMAP 与 SMTP 共用用户名和授权码；`tls/smtp_tls` 为 `ssl/starttls/none`，生产应使用 TLS。授权码 AES-GCM 加密入库，返回 `********`；更新时该值或省略 password 表示保留。SMTP 配置仅保存，本阶段测试连接只验证 IMAP，不发送邮件。被上游账号或邮件规则引用的邮箱不能删除。

邮件监听每秒重新读取账号启停/连接配置，变更后重连。优先 IDLE，十五秒校准扫描；无 IDLE 则十五秒轮询。断线按 1/2/4…60 秒退避，稳定连接一分钟后重置。每次连接扫描最近一天、最多五百封邮件；验证码等待历史仅保留内存中最近二百五十六封。首次扫描的历史信件可用于按 since 过滤的提码，但不会触发历史通知（启动所在秒的新信除外）。UID 增量、UIDVALIDITY 变化后重新校准；进程重启不持久化邮件游标，业务同步仍靠工单唯一键去重。处理 MIME base64、quoted-printable、中文主题和 HTML 文本。

规则示例：

```json
{"name":"维修新单","mail_account_id":1,"from":"notifications@example\\.com","subject":"维修|待认领","kind":"fault_repair","enabled":true}
```

正则使用 Go RE2；from/subject 为空表示任意，code 默认为 `\b\d{6}\b`，可使用第一个捕获组。等待器按邮箱账号和 since 区分验证码，since 使用 IMAP 秒级时间精度。

## Dora 登录配置与边界

```json
{"name":"Dora 供应商","type":"dora","email":"tickets@example.com","mail_account_id":1,"filter":{"from":"授权确认后的发件人正则","subject":"验证码","code":"\\b\\d{6}\\b"},"iam_base":"https://已确认IAM主机","business_base":"https://已确认业务主机","check_url":"https://已确认保活地址","account_type":"email","enabled":true}
```

创建 Dora 账号必须明确提供 IAM、业务和保活 URL，并关联已启用的 IMAP 账号；没有生产默认地址。配置只允许可信管理员操作，服务器必须能访问所配置主机。

- 已知发码路径：`POST {iam_base}/api/v1/login/sms`，`{account,account_type:"email",scene:"login"}`，从顶层或 `data.token` 取回 token。
- 已知验码路径：`POST {iam_base}/api/v1/login/account/code`，`{code,token,account_type}`；验码的 account_type 可配置。
- 使用 Cookie Jar 和 `csrftoken`→`X-CSRFToken`。Cookie Jar 与 challenge 均加密落库；Redis/内存缓存 Cookie 加密副本一小时，手填验证码通过加密的短期缓存传给正在持锁的流程。
- **待抓包确认**：响应业务字段、IAM 到业务站的跳转/会话建立、CSRF 首次初始化、正确的 account_type 与保活 URL。现有骨架不会自行拼接或尝试未知地址；保活仅对明确配置 URL GET，以 HTTP 401/403 判定会话失效。
- 状态：未登录→发码中→等待验证码→登录中→在线；保活失效标过期并重登；失败计数累计，连续三次后停止自动登录，并向启用管理员写站内告警。管理员显式登录或手填验证码仍可重试，成功清零。每分钟保活/重试在线、过期和失败且次数未满三次的账号。
- 同一账号登录使用十二分钟分布式锁，登录最多十分钟。手填接口在已有登录锁时交付验证码；没有活跃锁时可继续未过期的已发码流程。前端发起登录应保留该请求，同时通过另一个请求查状态或手填，不应在客户端设置短超时。
- 未完成流程崩溃后，十二分钟后标过期再自动重试。错误公开描述不含 token、Cookie、授权码或邮件验证码。

## 采集器与调度

预注册：fault_repair 故障维修、server_offline 服务器下架、server_reconfig 服务器改配、logistics 物流、inspection 机房/服务器巡检、rack_operation 机柜操作、assist 协查、room_access 入室、incident_review 事故复盘、rack_power 机柜上下电、security 安全工单、event 事件、maintenance 维护、change 变更。

Collector 提供 Kind/List/Detail/Normalize，注册表可替换真实实现；List 使用最近成功时间作为 since，Filters 通过 Session 传递。真实列表/详情/字段映射均留待授权抓包后实现。

启用 kind 后，调度器每秒检查到期配置，调度、手动和邮件都调用同一 Trigger。相同 kind 三秒去抖；执行另持十二分钟锁、十一分钟超时，跨实例互斥（多实例必须共用 Redis）。不同 kind 并行执行。每次真正执行写同步日志，记录开始/结束/尝试条数/新建/变更/错误；锁冲突、禁用、去抖或退避跳过不计一次同步。List 会话失效时重新登录并重试一次；Upsert 仍负责工单和通知事件的去重。

间隔均为 10~600 秒，work_start/work_end 为 HH:MM，可跨午夜，timezone 为 IANA 时区；都为空时用 interval_seconds，否则根据工作时段选 work_interval_seconds/off_interval_seconds。失败次数增加，下一次取正常间隔与 10/20/40…600 秒指数退避的较大值。邮件和手动触发也尊重错误退避。

开发配置示例：

```json
{"enabled":true,"mock":true,"interval_seconds":10,"work_interval_seconds":10,"off_interval_seconds":10,"work_start":"09:00","work_end":"18:00","timezone":"Asia/Shanghai","filters":{"site":"模拟机房"}}
```

仅 `DEV_MODE=true` 可启用 mock Collector 或 mock Authenticator。任意预注册 kind 可使用 mock，随机新建工单或推进“待认领→处理中→已完成”；mock 过滤配置支持 site。非开发模式即使数据库残留 mock 配置也拒绝执行。mock 登录账号 `type:"mock"`、任意合法测试邮箱、enabled:true，`POST /login` 自动使用固定 `123456`，无需真实 IMAP，成功状态在线。创建站内通知渠道和规则后启用 fault_repair，等至少一个周期，即可在 `/api/tickets` 与 `/api/notifications` 查看链路结果。

## 本地测试

`go vet ./... && go test ./...`。mailwatch 使用 go-imap 内存 backend 的本地 TCP 服务和 IDLE 更新；Dora 使用 httptest；scheduler 覆盖触发去抖、执行锁、退避、会话重登、空同步与未实现错误；mock→Upsert→站内通知端到端测试；管理员接口覆盖权限、打码保留、加密落库、引用保护与开发限制。不会访问真实 Dora、真实邮箱或发送真实外部通知。
