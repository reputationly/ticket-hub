export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: "include",
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 401 && !path.startsWith("/auth/login")) {
    window.dispatchEvent(new Event("session-expired"));
  }
  const result = await response
    .json()
    .catch(() => ({ message: "服务器响应格式错误" }));
  if (!response.ok)
    throw new APIError(
      response.status,
      result.message || "请求失败，请稍后重试",
    );
  return result.data as T;
}
export function query(values: Record<string, unknown>) {
  const p = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "")
      p.set(key, String(value));
  });
  return p.toString();
}
export const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "请求失败，请稍后重试";
export interface User {
  id: number;
  username: string;
  display_name: string;
  role: "admin" | "user";
  phone: string;
  email: string;
  feishu_open_id: string;
  enabled: boolean;
}
export interface Settings {
  site_name: string;
  logo_url: string;
  public_url?: string;
  session_hours?: number;
}
export interface Ticket {
  id: number;
  kind: string;
  upstream_id: string;
  title: string;
  status: string;
  node: string;
  site: string;
  assignee: string;
  priority: string;
  deadline: string | null;
  created_at: string;
  updated_at: string;
  created_at_upstream: string | null;
  updated_at_upstream: string | null;
  raw: unknown;
}
export interface TicketEvent {
  id: number;
  type: string;
  created_at: string;
  before: unknown;
  after: unknown;
}
export interface Notice {
  id: number;
  ticket_id: number;
  title: string;
  content: string;
  read: boolean;
  created_at: string;
}
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
export interface Catalog {
  kinds: { value: string; label: string }[];
  sites: string[];
  statuses: string[];
}
// 配置页的字段由各资源 schema 决定，运行时交由对应后端校验。
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type Data = Record<string, any>;
export const events = [
  { value: "ticket.created", label: "新建工单" },
  { value: "ticket.status_changed", label: "状态变更" },
  { value: "ticket.updated", label: "信息更新" },
];
export const channelNames: Record<string, string> = {
  inapp: "站内消息",
  email: "邮件",
  sms_aliyun: "阿里云短信",
  wecom_bot: "企业微信机器人",
  feishu_bot: "飞书机器人",
};
export const dateText = (value: unknown) =>
  value
    ? new Date(String(value)).toLocaleString("zh-CN", { hour12: false })
    : "—";
