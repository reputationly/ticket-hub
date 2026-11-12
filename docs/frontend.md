# 响应式前端

技术栈：React 18、TypeScript、Vite、Ant Design 5、React Router、pnpm。所有接口由 `src/api.ts` 的 fetch 封装调用，携带 HttpOnly Cookie，401 清理登录态并跳转登录页；没有本地存储 JWT。

## 开发与构建

```sh
pnpm --dir web install
make run
# 另一个终端
pnpm --dir web dev
```

Vite 默认运行在 `http://127.0.0.1:5173`，`/api` 代理到 `http://127.0.0.1:8080`。代理保留 Host，让后端同源校验正常工作；本地 HTTP 开发不要启用 `COOKIE_SECURE`。

```sh
pnpm --dir web lint
pnpm --dir web build
go test ./...
make build
```

`make build` 顺序构建前端和 Go 二进制，`web/dist` 通过 Go embed 打包，无需额外静态服务器。未构建的源码仍带占位文件，因此纯后端测试可独立运行。`web/public/placeholder.txt` 在前端构建时原样复制，保留该占位。

## 页面与接口

- 工单：筛选、桌面分页表格、手机卡片滚动加载、详情、事件快照及原始 JSON。
- 消息：SSE 更新消息和未读角标，标记已读；用户可主动授权浏览器通知，默认关闭。
- 我的：提醒订阅、跨午夜免打扰、紧急突破、群 @、资料及修改密码。
- 管理：站点、用户、邮箱、上游、采集器、邮件规则、五类渠道、提醒规则与模板预览、发送/同步/审计日志。上游及采集器每四秒刷新状态；自动登录可另开页面观察状态并手填验证码。
- ≥768px 使用侧栏及表格；<768px 使用顶部品牌、底部 Tab、卡片、单列表单和全屏编辑。管理 Tab 只向管理员显示。所有工单只读，不提供认领、处理或回写入口。

新增 `GET /api/site` 公开站点名和 Logo（不暴露其他设置），让登录页也能显示品牌；`GET /api/catalog` 需登录，提供预注册工单类型及数据库中的机房/状态选项。修改站点设置立即刷新品牌。

管理表单定义在 `src/schemas.tsx`，复用 `src/management.tsx` 的列表与编辑器。配置密钥按后端的 `********` 保留约定处理，渠道类型创建后不可改变。个人订阅只展示管理员已启用的短信、邮件及站内渠道。

## Chromium 截图与联调

先以 `DEV_MODE=true` 启动本地服务，在管理页创建 mock 上游并登录，启用多种 kind 的 mock 采集器；创建启用的 inapp 渠道和指向管理员的提醒规则。等待采集产生工单和站内消息。不要启用真实外部渠道。

```sh
pnpm --dir web exec playwright install chromium
# 在环境中设置 ADMIN_PASSWORD；可选 BASE_URL、ADMIN_USERNAME、SCREEN_DIR
pnpm --dir web screenshots
```

脚本要求已有演示数据和站内提醒规则，输出桌面 1440、手机 390 的八张截图，默认在 `~/ticket-hub-validation/screens/`（不进仓库）。额外检查无横向溢出、全屏抽屉、开发工单注入经 SSE 到达消息页、退出后 401 跳转登录；不会发送真实外部通知。`--mobile` 只重拍手机场景。
