import type { ReactNode } from "react";
import {
  ApiOutlined,
  AuditOutlined,
  CloudSyncOutlined,
  FileTextOutlined,
  MailOutlined,
  NotificationOutlined,
  SendOutlined,
  SettingOutlined,
  TeamOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { channelNames, events, type Data } from "./api";
export interface Option {
  label: string;
  value: string | number;
}
export interface Field {
  name: string;
  label: string;
  type?:
    | "password"
    | "number"
    | "switch"
    | "select"
    | "tags"
    | "multi"
    | "textarea"
    | "json";
  required?: boolean;
  options?: Option[];
  source?:
    "kinds" | "sites" | "statuses" | "users" | "groups" | "mail" | "upstream";
  min?: number;
  max?: number;
  hint?: string;
  show?: (data: Data, editing: boolean) => boolean;
}
export interface Column {
  key: string;
  label: string;
  format?: "time" | "status" | "bool" | "json" | "channel";
}
export interface Resource {
  key: string;
  title: string;
  shortTitle?: string;
  description: string;
  icon: ReactNode;
  endpoint: string;
  paged?: boolean;
  readonly?: boolean;
  idKey?: string;
  poll?: boolean;
  fields: Field[];
  columns: Column[];
  defaults?: Data;
  filters?: Field[];
}
const bool: Field = { name: "enabled", label: "启用", type: "switch" };
const name: Field = { name: "name", label: "名称", required: true };
const tls: Option[] = [
  { value: "ssl", label: "SSL / TLS" },
  { value: "starttls", label: "STARTTLS" },
  { value: "none", label: "不加密（仅测试）" },
];
const personal = Object.entries(channelNames)
  .filter(([key]) => ["inapp", "email", "sms_aliyun"].includes(key))
  .map(([value, label]) => ({ value, label }));
const conditionFields: Field[] = [
  {
    name: "conditions.kinds",
    label: "工单类型",
    type: "multi",
    source: "kinds",
  },
  {
    name: "conditions.statuses",
    label: "工单状态",
    type: "tags",
    source: "statuses",
  },
  {
    name: "conditions.sites",
    label: "机房范围",
    type: "tags",
    source: "sites",
  },
  { name: "conditions.keyword", label: "标题 / 编号关键词" },
  {
    name: "conditions.event_types",
    label: "事件类型",
    type: "multi",
    options: events,
  },
];
const channelFields: Field[] = [
  {
    name: "config.access_key_id",
    label: "AccessKey ID",
    type: "password",
    required: true,
    show: (d) => d.type === "sms_aliyun",
  },
  {
    name: "config.access_key_secret",
    label: "AccessKey Secret",
    type: "password",
    required: true,
    show: (d) => d.type === "sms_aliyun",
  },
  {
    name: "config.sign_name",
    label: "短信签名",
    required: true,
    show: (d) => d.type === "sms_aliyun",
  },
  {
    name: "config.template_code",
    label: "短信模板 Code",
    required: true,
    show: (d) => d.type === "sms_aliyun",
  },
  {
    name: "config.template_params",
    label: "短信变量映射 JSON",
    type: "json",
    hint: '例如 {"name":"title","state":"status"}',
    show: (d) => d.type === "sms_aliyun",
  },
  {
    name: "config.host",
    label: "SMTP 主机",
    required: true,
    show: (d) => d.type === "email",
  },
  {
    name: "config.port",
    label: "SMTP 端口（字符串）",
    required: true,
    show: (d) => d.type === "email",
  },
  {
    name: "config.username",
    label: "SMTP 用户名",
    show: (d) => d.type === "email",
  },
  {
    name: "config.password",
    label: "SMTP 授权码",
    type: "password",
    show: (d) => d.type === "email",
  },
  {
    name: "config.from",
    label: "发件人",
    required: true,
    show: (d) => d.type === "email",
  },
  {
    name: "config.tls",
    label: "传输加密",
    type: "select",
    options: tls,
    required: true,
    show: (d) => d.type === "email",
  },
  {
    name: "config.html_template",
    label: "邮件 HTML 模板",
    type: "textarea",
    hint: "可使用 {{.Title}}、{{.Content}}、{{.TicketID}}，留空使用默认模板",
    show: (d) => d.type === "email",
  },
  {
    name: "config.webhook",
    label: "机器人 Webhook",
    type: "password",
    required: true,
    show: (d) => ["wecom_bot", "feishu_bot"].includes(d.type),
  },
  {
    name: "config.secret",
    label: "飞书签名密钥（可选）",
    type: "password",
    show: (d) => d.type === "feishu_bot",
  },
];
export const resources: Resource[] = [
  {
    key: "users",
    title: "用户管理",
    description: "管理团队成员、权限与账号安全。",
    icon: <TeamOutlined />,
    endpoint: "/admin/users",
    paged: true,
    defaults: { role: "user", enabled: true },
    fields: [
      { name: "username", label: "用户名", required: true },
      {
        name: "password",
        label: "初始密码",
        type: "password",
        required: true,
        hint: "12~72 字节",
        show: (_, edit) => !edit,
      },
      { name: "display_name", label: "显示名称" },
      {
        name: "role",
        label: "角色",
        type: "select",
        required: true,
        options: [
          { label: "普通用户", value: "user" },
          { label: "管理员", value: "admin" },
        ],
      },
      { name: "phone", label: "手机号" },
      { name: "email", label: "邮箱" },
      bool,
    ],
    columns: [
      { key: "username", label: "用户名" },
      { key: "display_name", label: "姓名" },
      { key: "role", label: "角色" },
      { key: "phone", label: "手机号" },
      { key: "email", label: "邮箱" },
      { key: "enabled", label: "启用", format: "bool" },
    ],
    filters: [
      { name: "username", label: "用户名" },
      {
        name: "role",
        label: "角色",
        type: "select",
        options: [
          { label: "管理员", value: "admin" },
          { label: "普通用户", value: "user" },
        ],
      },
    ],
  },
  {
    key: "mail-accounts",
    title: "邮箱账号",
    description: "配置 IMAP 收件与 SMTP 账号，授权码始终加密存储。",
    icon: <MailOutlined />,
    endpoint: "/admin/mail-accounts",
    defaults: {
      enabled: true,
      port: 993,
      tls: "ssl",
      smtp_port: 465,
      smtp_tls: "ssl",
    },
    fields: [
      name,
      { name: "host", label: "IMAP 主机", required: true },
      {
        name: "port",
        label: "IMAP 端口",
        type: "number",
        required: true,
        min: 1,
        max: 65535,
      },
      {
        name: "tls",
        label: "IMAP 加密",
        type: "select",
        options: tls,
        required: true,
      },
      { name: "username", label: "用户名", required: true },
      {
        name: "password",
        label: "授权码",
        type: "password",
        hint: "编辑时 ******** 表示保留原授权码",
        required: true,
      },
      { name: "smtp_host", label: "SMTP 主机" },
      {
        name: "smtp_port",
        label: "SMTP 端口",
        type: "number",
        min: 0,
        max: 65535,
      },
      { name: "smtp_tls", label: "SMTP 加密", type: "select", options: tls },
      bool,
    ],
    columns: [
      { key: "name", label: "邮箱名称" },
      { key: "username", label: "登录账号" },
      { key: "host", label: "IMAP 主机" },
      { key: "port", label: "端口" },
      { key: "tls", label: "加密" },
      { key: "enabled", label: "启用", format: "bool" },
    ],
  },
  {
    key: "upstream",
    title: "上游账号",
    description:
      "查看会话状态、自动登录或提交验证码。真实 Dora 配置需以授权抓包为准。",
    icon: <ApiOutlined />,
    endpoint: "/admin/upstream",
    paged: true,
    poll: true,
    defaults: {
      type: "dora",
      enabled: true,
      account_type: "email",
      filter: { code: "\\b\\d{6}\\b" },
    },
    fields: [
      name,
      {
        name: "type",
        label: "认证类型",
        type: "select",
        required: true,
        options: [
          { value: "dora", label: "Dora（待抓包确认）" },
          { value: "mock", label: "Mock（仅开发模式）" },
        ],
      },
      { name: "email", label: "登录邮箱", required: true },
      {
        name: "mail_account_id",
        label: "关联 IMAP 账号",
        type: "select",
        source: "mail",
        show: (d) => d.type === "dora",
        required: true,
      },
      {
        name: "filter.from",
        label: "验证码发件人正则",
        show: (d) => d.type === "dora",
      },
      {
        name: "filter.subject",
        label: "验证码主题正则",
        show: (d) => d.type === "dora",
      },
      {
        name: "filter.code",
        label: "验证码提取正则",
        show: (d) => d.type === "dora",
      },
      {
        name: "iam_base",
        label: "IAM Base URL",
        show: (d) => d.type === "dora",
        required: true,
      },
      {
        name: "business_base",
        label: "业务 Base URL",
        show: (d) => d.type === "dora",
        required: true,
      },
      {
        name: "check_url",
        label: "只读保活 URL",
        show: (d) => d.type === "dora",
        required: true,
      },
      {
        name: "account_type",
        label: "验码 account_type",
        show: (d) => d.type === "dora",
      },
      bool,
    ],
    columns: [
      { key: "name", label: "账号名称" },
      { key: "email", label: "登录邮箱" },
      { key: "type", label: "认证方式" },
      { key: "state", label: "登录状态", format: "status" },
      { key: "last_login_at", label: "最后登录", format: "time" },
      { key: "failures", label: "连续失败" },
      { key: "last_error", label: "最后错误" },
    ],
  },
  {
    key: "collectors",
    title: "采集配置",
    description: "十四类工单独立调度。真实采集器待抓包，Mock 仅限开发环境。",
    icon: <CloudSyncOutlined />,
    endpoint: "/admin/collectors",
    idKey: "kind",
    poll: true,
    defaults: {},
    fields: [
      bool,
      { name: "mock", label: "使用模拟采集（DEV_MODE）", type: "switch" },
      {
        name: "upstream_account_id",
        label: "关联上游账号",
        type: "select",
        source: "upstream",
      },
      {
        name: "interval_seconds",
        label: "默认间隔（秒）",
        type: "number",
        min: 10,
        max: 600,
        required: true,
      },
      {
        name: "work_interval_seconds",
        label: "工作时段间隔（秒）",
        type: "number",
        min: 10,
        max: 600,
        required: true,
      },
      {
        name: "off_interval_seconds",
        label: "非工作时段间隔（秒）",
        type: "number",
        min: 10,
        max: 600,
        required: true,
      },
      {
        name: "work_start",
        label: "工作开始时间",
        hint: "HH:MM，可跨午夜；两项留空使用默认间隔",
      },
      { name: "work_end", label: "工作结束时间" },
      { name: "timezone", label: "时区" },
      {
        name: "filters",
        label: "过滤条件 JSON",
        type: "json",
        hint: 'Mock 支持 {"site":"模拟机房"}',
      },
    ],
    columns: [
      { key: "name", label: "工单类型" },
      { key: "enabled", label: "启用", format: "bool" },
      { key: "mock", label: "模拟模式", format: "bool" },
      { key: "interval_seconds", label: "间隔 / 秒" },
      { key: "last_success_at", label: "最后成功", format: "time" },
      { key: "last_error", label: "最后错误" },
    ],
  },
  {
    key: "mail-rules",
    title: "邮件触发规则",
    description: "通知邮件命中规则后，即刻触发对应类型的同步。",
    icon: <ThunderboltOutlined />,
    endpoint: "/admin/mail-rules",
    paged: true,
    defaults: { enabled: true },
    fields: [
      name,
      {
        name: "mail_account_id",
        label: "收件邮箱账号",
        type: "select",
        source: "mail",
        required: true,
      },
      { name: "from", label: "发件人正则" },
      { name: "subject", label: "主题正则" },
      {
        name: "kind",
        label: "触发的工单类型",
        type: "select",
        source: "kinds",
        required: true,
      },
      bool,
    ],
    columns: [
      { key: "name", label: "规则名称" },
      { key: "mail_account_id", label: "邮箱 ID" },
      { key: "from", label: "发件人正则" },
      { key: "subject", label: "主题正则" },
      { key: "kind", label: "采集类型" },
      { key: "enabled", label: "启用", format: "bool" },
    ],
  },
  {
    key: "channels",
    title: "通知渠道",
    description: "将重要变化送达正确的地方。密钥加密保存，支持独立测试。",
    icon: <NotificationOutlined />,
    endpoint: "/admin/channels",
    defaults: { enabled: true, type: "inapp", config: {} },
    fields: [
      name,
      {
        name: "type",
        label: "渠道类型",
        type: "select",
        required: true,
        options: Object.entries(channelNames).map(([value, label]) => ({
          value,
          label,
        })),
      },
      bool,
      ...channelFields,
    ],
    columns: [
      { key: "name", label: "渠道名称" },
      { key: "type", label: "渠道类型", format: "channel" },
      { key: "enabled", label: "启用", format: "bool" },
      { key: "config", label: "配置摘要", format: "json" },
      { key: "updated_at", label: "最近更新", format: "time" },
    ],
  },
  {
    key: "rules",
    title: "提醒规则",
    description: "以条件匹配工单，为指定成员和群机器人编排提醒。",
    icon: <SettingOutlined />,
    endpoint: "/admin/rules",
    paged: true,
    defaults: {
      enabled: true,
      conditions: {},
      actions: {
        user_ids: [],
        group_channel_ids: [],
        personal_channels: ["inapp"],
        template: "",
      },
    },
    fields: [
      name,
      bool,
      ...conditionFields,
      {
        name: "actions.user_ids",
        label: "目标用户",
        type: "multi",
        source: "users",
      },
      {
        name: "actions.group_channel_ids",
        label: "群机器人渠道",
        type: "multi",
        source: "groups",
      },
      {
        name: "actions.personal_channels",
        label: "个人渠道类型",
        type: "multi",
        options: personal,
      },
      {
        name: "actions.template",
        label: "消息模板",
        type: "textarea",
        hint: "Go text/template：{{.Ticket.Title}}、{{.Event.Type}}、{{.Link}}；留空使用默认中文模板",
      },
    ],
    columns: [
      { key: "name", label: "规则名称" },
      { key: "enabled", label: "启用", format: "bool" },
      { key: "conditions", label: "匹配条件", format: "json" },
      { key: "actions", label: "通知动作", format: "json" },
      { key: "updated_at", label: "最近更新", format: "time" },
    ],
  },
  {
    key: "notify-jobs",
    title: "发送日志",
    description: "追踪通知投递、失败原因与重试，可手动重新入队。",
    icon: <SendOutlined />,
    endpoint: "/admin/notify-jobs",
    paged: true,
    readonly: true,
    fields: [],
    filters: [
      {
        name: "status",
        label: "发送状态",
        type: "select",
        options: [
          { value: "pending", label: "待发送" },
          { value: "processing", label: "处理中" },
          { value: "sent", label: "已送达" },
          { value: "failed", label: "失败" },
        ],
      },
      {
        name: "channel_type",
        label: "渠道类型",
        type: "select",
        options: Object.entries(channelNames).map(([value, label]) => ({
          value,
          label,
        })),
      },
      { name: "user_id", label: "用户 ID", type: "number" },
    ],
    columns: [
      { key: "id", label: "任务 ID" },
      { key: "channel_type", label: "渠道", format: "channel" },
      { key: "delivery.title", label: "消息标题" },
      { key: "status", label: "状态", format: "status" },
      { key: "attempts", label: "尝试次数" },
      { key: "last_error", label: "最后错误" },
      { key: "created_at", label: "创建时间", format: "time" },
    ],
  },
  {
    key: "sync-logs",
    title: "同步日志",
    description: "每次实际同步的开始、结束、工单变化与错误记录。",
    icon: <FileTextOutlined />,
    endpoint: "/admin/sync-logs",
    paged: true,
    readonly: true,
    fields: [],
    filters: [
      { name: "kind", label: "工单类型", type: "select", source: "kinds" },
      {
        name: "status",
        label: "同步结果",
        type: "select",
        options: [
          { value: "success", label: "成功" },
          { value: "error", label: "失败" },
        ],
      },
    ],
    columns: [
      { key: "kind", label: "工单类型" },
      { key: "source", label: "触发来源" },
      { key: "started_at", label: "开始时间", format: "time" },
      { key: "finished_at", label: "结束时间", format: "time" },
      { key: "count", label: "总条数" },
      { key: "created", label: "新建" },
      { key: "changed", label: "变更" },
      { key: "error", label: "错误" },
    ],
  },
  {
    key: "audit-logs",
    title: "审计日志",
    description: "管理写操作留痕，敏感凭据不会写入审计内容。",
    icon: <AuditOutlined />,
    endpoint: "/admin/audit-logs",
    paged: true,
    readonly: true,
    fields: [],
    columns: [
      { key: "id", label: "记录 ID" },
      { key: "user_id", label: "操作者 ID" },
      { key: "action", label: "操作" },
      { key: "target", label: "对象" },
      { key: "ip", label: "来源 IP" },
      { key: "created_at", label: "时间", format: "time" },
    ],
  },
];
