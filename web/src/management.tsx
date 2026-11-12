import { useEffect, useState, type ReactNode } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import DatePicker from "antd/es/date-picker";
import Drawer from "antd/es/drawer";
import Empty from "antd/es/empty";
import Form from "antd/es/form";
import Input from "antd/es/input";
import InputNumber from "antd/es/input-number";
import Modal from "antd/es/modal";
import Pagination from "antd/es/pagination";
import Popconfirm from "antd/es/popconfirm";
import Select from "antd/es/select";
import Space from "antd/es/space";
import Switch from "antd/es/switch";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import message from "antd/es/message";
import {
  ArrowRightOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  SearchOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { Link } from "react-router-dom";
import {
  api,
  channelNames,
  dateText,
  errorText,
  query,
  type Data,
  type Page,
  type Settings,
  type Ticket,
} from "./api";
import { Blank, Failure, PageHead, Refresh, Status, useMobile } from "./common";
import { useHub } from "./context";
import {
  resources,
  type Column,
  type Field,
  type Option,
  type Resource,
} from "./schemas";

function get(data: Data, path: string): unknown {
  return path.split(".").reduce((v: Data, k: string) => v?.[k], data);
}
function put(data: Data, path: string, value: unknown) {
  const parts = path.split(".");
  let target = data;
  parts.slice(0, -1).forEach((k) => {
    target[k] ??= {};
    target = target[k];
  });
  target[parts[parts.length - 1]] = value;
}
type Lookups = Record<string, Option[]>;
function Control({
  field,
  options,
  disabled,
  ...controlProps
}: {
  field: Field;
  options: Option[];
  disabled?: boolean;
} & Data) {
  switch (field.type) {
    case "switch":
      return <Switch {...controlProps} disabled={disabled} />;
    case "number":
      return (
        <InputNumber
          {...controlProps}
          style={{ width: "100%" }}
          min={field.min}
          max={field.max}
          disabled={disabled}
        />
      );
    case "password":
      return (
        <Input.Password
          {...controlProps}
          autoComplete="new-password"
          disabled={disabled}
        />
      );
    case "json":
    case "textarea":
      return (
        <Input.TextArea
          {...controlProps}
          rows={field.type === "json" ? 4 : 5}
          spellCheck={false}
        />
      );
    case "select":
    case "multi":
    case "tags":
      return (
        <Select
          {...controlProps}
          allowClear
          showSearch
          optionFilterProp="label"
          disabled={disabled}
          mode={
            field.type === "multi"
              ? "multiple"
              : field.type === "tags"
                ? "tags"
                : undefined
          }
          options={options}
          placeholder={`请选择${field.label}`}
        />
      );
    default:
      return <Input {...controlProps} disabled={disabled} />;
  }
}
function FieldItem({
  field,
  lookups,
  disabled,
}: {
  field: Field;
  lookups: Lookups;
  disabled?: boolean;
}) {
  return (
    <Form.Item
      name={field.name.split(".")}
      label={field.label}
      valuePropName={field.type === "switch" ? "checked" : "value"}
      preserve={false}
      extra={field.hint}
      className={
        field.type === "textarea" || field.type === "json"
          ? "full-width"
          : undefined
      }
      rules={[
        ...(field.required
          ? [{ required: true, message: `请填写${field.label}` }]
          : []),
        ...(field.type === "json"
          ? [
              {
                validator: async (_: unknown, value: unknown) => {
                  if (value === "" || value == null) return;
                  try {
                    const v = JSON.parse(String(value));
                    if (!v || Array.isArray(v) || typeof v !== "object")
                      throw new Error();
                  } catch {
                    throw new Error("请输入有效 JSON 对象");
                  }
                },
              },
            ]
          : []),
        ...(field.name === "email"
          ? [{ type: "email" as const, message: "请输入合法邮箱" }]
          : []),
      ]}
    >
      <Control
        field={field}
        options={field.options || lookups[field.source || ""] || []}
        disabled={disabled}
      />
    </Form.Item>
  );
}
function Cell({ column, row }: { column: Column; row: Data }) {
  const { catalog } = useHub();
  const v = get(row, column.key);
  if (column.format === "time")
    return <span className="small">{dateText(v)}</span>;
  if (column.format === "status") return <Status value={v} />;
  if (column.format === "bool")
    return <Tag color={v ? "green" : "default"}>{v ? "已启用" : "已停用"}</Tag>;
  if (column.format === "channel")
    return <span>{channelNames[String(v)] || String(v)}</span>;
  if (column.format === "json") {
    const text =
      column.key === "config"
        ? Object.entries((v || {}) as Data)
            .map(
              ([k, value]) =>
                `${k}: ${typeof value === "object" ? JSON.stringify(value) : String(value)}`,
            )
            .join(" · ") || "无需额外配置"
        : JSON.stringify(v);
    return (
      <span className="config-summary" title={text}>
        {text}
      </span>
    );
  }
  if (column.key === "kind")
    return (
      <Tag>{catalog.kinds.find((k) => k.value === v)?.label || String(v)}</Tag>
    );
  if (column.key === "role")
    return <span>{v === "admin" ? "管理员" : "普通用户"}</span>;
  return (
    <span className={column.key.includes("error") ? "error-text" : undefined}>
      {v == null || v === "" ? "—" : String(v)}
    </span>
  );
}

export function AdminMenu() {
  return (
    <>
      <PageHead
        title="管理控制台"
        description="连接上游、配置提醒，让团队协作有条不紊。"
      />
      <div className="admin-intro">
        <SafetyCertificateOutlined />
        <div>
          <strong>只读同步，安全掌控</strong>
          <p>本系统不会在上游认领、处理或关闭任何工单。</p>
        </div>
      </div>
      <div className="admin-grid">
        {[
          {
            key: "settings",
            title: "站点设置",
            description: "站点名称、标志与访问策略",
            icon: <SettingOutlined />,
          },
          ...resources,
        ].map((r) => (
          <Link key={r.key} to={`/admin/${r.key}`} className="admin-tile">
            <span className="admin-tile-icon">{r.icon}</span>
            <div>
              <h3>{r.title}</h3>
              <p>{r.description}</p>
            </div>
            <ArrowRightOutlined />
          </Link>
        ))}
      </div>
    </>
  );
}
export function SiteSettings() {
  const { reloadSite } = useHub();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    api<Settings>("/admin/settings")
      .then((v) => {
        if (active) {
          form.setFieldsValue(v);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(errorText(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [form, reload]);
  return (
    <>
      <PageHead
        title="站点设置"
        description="管理站点品牌、访问地址和登录会话。"
      />
      <Card className="form-card" title="基础配置">
        <Failure error={error} retry={() => setReload((v) => v + 1)} />
        {loading ? (
          <Blank loading />
        ) : (
          <Form
            form={form}
            layout="vertical"
            onFinish={async (values) => {
              setSaving(true);
              try {
                await api("/admin/settings", "PUT", values);
                await reloadSite();
                message.success("站点设置已保存");
              } catch (e) {
                message.error(errorText(e));
              } finally {
                setSaving(false);
              }
            }}
          >
            <div className="form-grid">
              <Form.Item
                name="site_name"
                label="站点名称"
                rules={[{ required: true, max: 100 }]}
              >
                <Input />
              </Form.Item>
              <Form.Item
                name="logo_url"
                label="Logo URL"
                rules={[{ type: "url" }]}
              >
                <Input placeholder="https://…" />
              </Form.Item>
              <Form.Item
                name="public_url"
                label="对外访问地址"
                rules={[{ type: "url" }]}
                extra="用于通知中的工单链接"
              >
                <Input placeholder="https://tickets.example.com" />
              </Form.Item>
              <Form.Item
                name="session_hours"
                label="会话时长（小时）"
                rules={[{ required: true }]}
                extra="1~720 小时，对新登录生效"
              >
                <InputNumber min={1} max={720} />
              </Form.Item>
            </div>
            <Button type="primary" htmlType="submit" loading={saving}>
              保存站点设置
            </Button>
          </Form>
        )}
      </Card>
    </>
  );
}

export function ResourcePage({ resource: r }: { resource: Resource }) {
  const { catalog, user } = useHub();
  const mobile = useMobile();
  const [rows, setRows] = useState<Data[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Data>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [editing, setEditing] = useState<Data | null>(null);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState("");
  const [aux, setAux] = useState<{ type: string; row: Data } | null>(null);
  const [auxBusy, setAuxBusy] = useState(false);
  const [lookups, setLookups] = useState<Lookups>({});
  const [form] = Form.useForm();
  const [auxForm] = Form.useForm();
  const values = Form.useWatch([], form) || {};
  const filterKey = JSON.stringify(filters);
  const [poll, setPoll] = useState(0);
  const [operating, setOperating] = useState<Record<string, boolean>>({});
  useEffect(() => {
    if (!r.poll) return;
    const timer = window.setInterval(() => setPoll((v) => v + 1), 4000);
    return () => clearInterval(timer);
  }, [r.poll]);
  useEffect(() => {
    let active = true;
    if (!poll) setLoading(true);
    api<{ items: Data[]; total?: number }>(
      `${r.endpoint}?${query({ ...JSON.parse(filterKey), page, page_size: 20 })}`,
    )
      .then((data) => {
        if (active) {
          setRows(data.items || []);
          setTotal(data.total ?? data.items?.length ?? 0);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(errorText(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [r.endpoint, filterKey, page, reload, poll]);
  useEffect(() => {
    let active = true;
    void Promise.all([
      api<{ items: Data[] }>("/admin/users?page_size=100"),
      api<{ items: Data[] }>("/admin/channels"),
      api<{ items: Data[] }>("/admin/mail-accounts"),
      api<{ items: Data[] }>("/admin/upstream?page_size=100"),
    ])
      .then(([users, channels, mail, upstream]) => {
        if (active)
          setLookups({
            users: (users.items || [])
              .filter((u) => u.enabled)
              .map((u) => ({
                label: u.display_name || u.username,
                value: u.id,
              })),
            groups: (channels.items || [])
              .filter(
                (c) =>
                  c.enabled && ["wecom_bot", "feishu_bot"].includes(c.type),
              )
              .map((c) => ({ label: c.name, value: c.id })),
            mail: (mail.items || []).map((a) => ({
              label: a.name,
              value: a.id,
            })),
            upstream: (upstream.items || []).map((a) => ({
              label: a.name,
              value: a.id,
            })),
          });
      })
      .catch((e) => message.error(errorText(e)));
    return () => {
      active = false;
    };
  }, [r.key, reload]);
  const options: Lookups = {
    ...lookups,
    kinds: catalog.kinds,
    sites: catalog.sites.map((v) => ({ value: v, label: v })),
    statuses: catalog.statuses.map((v) => ({ value: v, label: v })),
  };
  const openEditor = (row: Data | null) => {
    const data = structuredClone(row || r.defaults || {});
    r.fields
      .filter((f) => f.type === "json")
      .forEach((f) => {
        const v = get(data, f.name);
        if (v !== undefined) put(data, f.name, JSON.stringify(v, null, 2));
      });
    setEditing(row);
    form.resetFields();
    form.setFieldsValue(data);
    setPreview("");
    setOpen(true);
  };
  const save = async (values: Data) => {
    setSaving(true);
    try {
      const body = structuredClone(values);
      r.fields
        .filter(
          (f) => f.type === "json" && (!f.show || f.show(body, !!editing)),
        )
        .forEach((f) => {
          const v = get(body, f.name);
          put(body, f.name, v ? JSON.parse(String(v)) : {});
        });
      const key = editing?.[r.idKey || "id"];
      await api(
        `${r.endpoint}${editing ? `/${key}` : ""}`,
        editing ? "PUT" : "POST",
        body,
      );
      message.success("配置已保存");
      setOpen(false);
      setReload((v) => v + 1);
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setSaving(false);
    }
  };
  const action = async (row: Data, kind: string) => {
    const id = row[r.idKey || "id"];
    const key = `${id}:${kind}`;
    setOperating((v) => ({ ...v, [key]: true }));
    try {
      if (kind === "delete") {
        await api(`${r.endpoint}/${id}`, "DELETE");
        message.success("已删除");
      } else if (kind === "test-mail") {
        const v = await api<{ inbox_count: number }>(
          `${r.endpoint}/${id}/test`,
          "POST",
          {},
        );
        message.success(`连接成功，INBOX 共 ${v.inbox_count} 封邮件`);
      } else if (kind === "login") {
        message.info("自动登录已开始，可查询状态或手工提交验证码");
        await api(`${r.endpoint}/${id}/login`, "POST", {});
        message.success("登录成功");
      } else if (kind === "disable") {
        await api(`${r.endpoint}/${id}/disable`, "PUT", {});
        message.success("用户已禁用");
      } else {
        await api(
          `${r.endpoint}/${id}/${kind === "resend" ? "resend" : "sync"}`,
          "POST",
          {},
        );
        message.success(
          kind === "resend" ? "已重新入队" : "同步请求已接收，结果见同步日志",
        );
      }
      setReload((v) => v + 1);
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setOperating((v) => ({ ...v, [key]: false }));
    }
  };
  const openAux = (type: string, row: Data) => {
    auxForm.resetFields();
    auxForm.setFieldsValue(
      type === "test"
        ? {
            user_id: user.id,
            title: "工单中心测试通知",
            content: "这是一条测试提醒。",
          }
        : {},
    );
    setAux({ type, row });
  };
  const actions = (row: Data): ReactNode => (
    <Space wrap size="small">
      {!r.readonly && (
        <Button type="link" size="small" onClick={() => openEditor(row)}>
          配置
        </Button>
      )}
      {r.key === "mail-accounts" && (
        <Button
          size="small"
          type="link"
          loading={operating[`${row.id}:test-mail`]}
          onClick={() => void action(row, "test-mail")}
        >
          测试连接
        </Button>
      )}
      {r.key === "upstream" && (
        <>
          <Popconfirm
            title="确定向该账号发起登录？"
            description="真实账号会发送验证码邮件。"
            onConfirm={() => void action(row, "login")}
          >
            <Button
              type="link"
              size="small"
              loading={operating[`${row.id}:login`]}
            >
              自动登录
            </Button>
          </Popconfirm>
          <Button
            type="link"
            size="small"
            onClick={() => openAux("manual", row)}
          >
            手填验证码
          </Button>
        </>
      )}
      {r.key === "collectors" && (
        <Button
          type="link"
          size="small"
          disabled={!row.enabled}
          onClick={() => void action(row, "sync")}
        >
          立即同步
        </Button>
      )}
      {r.key === "channels" && (
        <Button
          type="link"
          size="small"
          disabled={!row.enabled}
          onClick={() => openAux("test", row)}
        >
          发送测试
        </Button>
      )}
      {r.key === "users" && (
        <>
          <Button
            type="link"
            size="small"
            onClick={() => openAux("reset", row)}
          >
            重置密码
          </Button>
          {row.enabled && (
            <Popconfirm
              title="禁用后现有会话将失效，确定？"
              onConfirm={() => void action(row, "disable")}
            >
              <Button type="link" danger size="small">
                禁用
              </Button>
            </Popconfirm>
          )}
        </>
      )}
      {r.key === "notify-jobs" && (
        <Popconfirm
          title="确定重新发送这条通知？"
          onConfirm={() => void action(row, "resend")}
        >
          <Button
            type="link"
            size="small"
            disabled={row.status === "processing"}
          >
            重发
          </Button>
        </Popconfirm>
      )}
      {!r.readonly && r.key !== "collectors" && (
        <Popconfirm
          title="确定删除此记录？"
          description="此操作不可撤销。"
          onConfirm={() => void action(row, "delete")}
        >
          <Button type="link" danger size="small">
            删除
          </Button>
        </Popconfirm>
      )}
    </Space>
  );
  const previewTemplate = async () => {
    try {
      const tickets = await api<Page<Ticket>>("/tickets?page_size=1");
      const sample = tickets.items?.[0] || {
        id: 0,
        title: "模板预览工单",
        kind: "fault_repair",
        status: "待认领",
        site: "示例机房",
      };
      const v = await api<{ content: string }>(
        "/admin/templates/preview",
        "POST",
        {
          template: form.getFieldValue(["actions", "template"]) || "",
          ticket: sample,
          event: { type: "ticket.created" },
        },
      );
      setPreview(v.content);
    } catch (e) {
      message.error(errorText(e));
    }
  };
  return (
    <>
      <Link className="back-link mobile-only" to="/admin">
        管理控制台
      </Link>
      <PageHead
        title={r.title}
        description={r.description}
        extra={
          <Space>
            <Refresh
              onClick={() => setReload((v) => v + 1)}
              loading={loading}
            />
            {!r.readonly && r.key !== "collectors" && (
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => openEditor(null)}
              >
                新建
                {r.key === "channels"
                  ? "渠道"
                  : r.key === "rules"
                    ? "规则"
                    : ""}
              </Button>
            )}
          </Space>
        }
      />
      {r.key === "channels" && (
        <div className="channel-overview">
          {Object.entries(channelNames).map(([key, label]) => (
            <div key={key}>
              <span className={`channel-mark ${key}`}>{label.slice(0, 1)}</span>
              <span>
                {label}
                <small>
                  {rows.filter((row) => row.type === key && row.enabled).length}{" "}
                  个已启用
                </small>
              </span>
            </div>
          ))}
        </div>
      )}
      {r.filters && (
        <Card className="filter-card">
          <Form
            layout="inline"
            className="log-filters"
            onFinish={(v) => {
              const { range, ...rest } = v;
              setFilters({
                ...rest,
                from: range?.[0]?.startOf("day").toISOString(),
                to: range?.[1]?.endOf("day").toISOString(),
              });
              setPage(1);
            }}
          >
            {r.filters.map((f) => (
              <FieldItem key={f.name} field={f} lookups={options} />
            ))}
            {r.key !== "users" && (
              <Form.Item name="range" label="时间范围">
                <DatePicker.RangePicker />
              </Form.Item>
            )}
            <Form.Item>
              <Button
                type="primary"
                htmlType="submit"
                icon={<SearchOutlined />}
              >
                筛选
              </Button>
            </Form.Item>
          </Form>
        </Card>
      )}
      <Failure error={error} retry={() => setReload((v) => v + 1)} />
      <Card
        className="list-panel"
        title={
          <span>
            {r.title} <span className="count-pill">{total}</span>
          </span>
        }
        extra={
          <span className="muted small">
            {r.poll ? "每 4 秒自动刷新" : "配置与运行记录"}
          </span>
        }
      >
        {mobile ? (
          <div className="management-cards">
            {rows.map((row) => (
              <article className="management-card" key={row[r.idKey || "id"]}>
                <h3>{row.name || row.username || `${r.title} #${row.id}`}</h3>
                <dl>
                  {r.columns
                    .filter((c) => !["name", "username"].includes(c.key))
                    .map((c) => (
                      <div key={c.key}>
                        <dt>{c.label}</dt>
                        <dd>
                          <Cell column={c} row={row} />
                        </dd>
                      </div>
                    ))}
                </dl>
                <div className="card-actions">{actions(row)}</div>
              </article>
            ))}
            {!rows.length && <Blank loading={loading} />}
          </div>
        ) : (
          <Table<Data>
            rowKey={r.idKey || "id"}
            dataSource={rows}
            loading={loading}
            scroll={{ x: 900 }}
            pagination={false}
            columns={[
              ...r.columns.map((c) => ({
                title: c.label,
                key: c.key,
                render: (_: unknown, row: Data) => (
                  <Cell column={c} row={row} />
                ),
              })),
              ...(r.readonly && r.key !== "notify-jobs"
                ? []
                : [
                    {
                      title: "操作",
                      key: "actions",
                      width: r.key === "upstream" ? 270 : 200,
                      render: (_: unknown, row: Data) => actions(row),
                    },
                  ]),
            ]}
            locale={{ emptyText: <Empty description="尚未添加记录" /> }}
          />
        )}
        {r.paged && (
          <Pagination
            className="pagination"
            current={page}
            total={total}
            pageSize={20}
            onChange={setPage}
            showSizeChanger={false}
            simple={mobile}
          />
        )}
      </Card>
      <Drawer
        open={open}
        onClose={() => setOpen(false)}
        width={mobile ? "100%" : 680}
        title={`${editing ? "配置" : "新建"}${r.title}`}
        extra={
          <Button type="primary" onClick={() => form.submit()} loading={saving}>
            保存配置
          </Button>
        }
        destroyOnClose
      >
        <Alert
          className="editor-note"
          type="info"
          showIcon
          message={
            r.key === "channels"
              ? "密钥字段 ******** 表示保留原值；渠道类型创建后不可变更。"
              : r.key === "collectors"
                ? "真实采集器尚未实现，返回“未实现，待抓包”；模拟采集仅在开发模式可用。"
                : "请检查配置后保存；所有管理变更均记录审计。"
          }
        />
        <Form
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(v) => void save(v)}
        >
          <div className="form-grid">
            {r.fields
              .filter((f) => !f.show || f.show(values, !!editing))
              .map((f) => (
                <FieldItem
                  key={f.name}
                  field={f}
                  lookups={options}
                  disabled={
                    r.key === "channels" && f.name === "type" && !!editing
                  }
                />
              ))}
          </div>
          {r.key === "rules" && (
            <>
              <Button onClick={() => void previewTemplate()}>
                预览消息模板
              </Button>
              {preview && <pre className="template-preview">{preview}</pre>}
            </>
          )}
        </Form>
      </Drawer>
      <Modal
        className={mobile ? "fullscreen-modal" : ""}
        open={!!aux}
        onCancel={() => setAux(null)}
        title={
          aux?.type === "reset"
            ? "重置用户密码"
            : aux?.type === "manual"
              ? "手工提交验证码"
              : "发送渠道测试"
        }
        okText={aux?.type === "test" ? "确认发送测试" : "确认提交"}
        confirmLoading={auxBusy}
        onOk={() => auxForm.submit()}
        destroyOnClose
      >
        <Form
          form={auxForm}
          layout="vertical"
          onFinish={async (v) => {
            if (!aux) return;
            setAuxBusy(true);
            try {
              await api(
                `${r.endpoint}/${aux.row.id}/${aux.type === "reset" ? "reset-password" : aux.type === "manual" ? "manual-code" : "test"}`,
                "POST",
                v,
              );
              message.success(
                aux.type === "test"
                  ? "测试任务已入队，请查看发送日志"
                  : "已提交",
              );
              setAux(null);
              setReload((n) => n + 1);
            } catch (e) {
              message.error(errorText(e));
            } finally {
              setAuxBusy(false);
            }
          }}
        >
          {aux?.type === "reset" ? (
            <Form.Item
              name="password"
              label="新密码"
              rules={[
                {
                  required: true,
                  min: 12,
                  max: 72,
                  message: "请输入 12~72 字节新密码",
                },
              ]}
            >
              <Input.Password />
            </Form.Item>
          ) : aux?.type === "manual" ? (
            <>
              <Alert
                message="请先发起自动登录，再填入收到的验证码。"
                type="info"
                showIcon
              />
              <Form.Item
                name="code"
                label="验证码"
                rules={[{ required: true }]}
              >
                <Input autoComplete="one-time-code" />
              </Form.Item>
            </>
          ) : (
            <>
              <Alert
                message="这会真实发送一条测试通知。请核实渠道与目标，避免误发。"
                type="warning"
                showIcon
              />
              {aux?.row.type === "inapp" ? (
                <Form.Item
                  name="user_id"
                  label="目标用户"
                  rules={[{ required: true }]}
                >
                  <Select options={lookups.users} />
                </Form.Item>
              ) : aux?.row.type === "email" ? (
                <Form.Item
                  name="email"
                  label="收件邮箱"
                  rules={[{ required: true, type: "email" }]}
                >
                  <Input />
                </Form.Item>
              ) : aux?.row.type === "sms_aliyun" ? (
                <Form.Item
                  name="phone"
                  label="手机号"
                  rules={[{ required: true }]}
                >
                  <Input />
                </Form.Item>
              ) : null}
              <Form.Item name="title" label="测试标题">
                <Input />
              </Form.Item>
              <Form.Item name="content" label="测试内容">
                <Input.TextArea rows={3} />
              </Form.Item>
              {aux?.row.type === "wecom_bot" && (
                <Form.Item
                  name="mentioned_mobile_list"
                  label="@ 手机号（输入后回车）"
                >
                  <Select mode="tags" />
                </Form.Item>
              )}
              {aux?.row.type === "feishu_bot" && (
                <Form.Item
                  name="mention_open_ids"
                  label="@ 飞书 Open ID（输入后回车）"
                >
                  <Select mode="tags" />
                </Form.Item>
              )}
            </>
          )}
        </Form>
      </Modal>
    </>
  );
}
