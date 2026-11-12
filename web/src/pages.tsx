import { useEffect, useRef, useState } from "react";
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Card,
  Checkbox,
  Collapse,
  DatePicker,
  Descriptions,
  Divider,
  Empty,
  Form,
  Input,
  Pagination,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Timeline,
  message,
} from "antd";
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  BellOutlined,
  CheckCircleOutlined,
  CheckSquareOutlined,
  FilterOutlined,
  LockOutlined,
  SearchOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { Link, useParams } from "react-router-dom";
import {
  api,
  channelNames,
  dateText,
  errorText,
  events,
  query,
  type Data,
  type Notice,
  type Page,
  type Settings,
  type Ticket,
  type TicketEvent,
  type User,
} from "./api";
import { Blank, Failure, PageHead, Refresh, Status, useMobile } from "./common";
import { useHub } from "./context";

export function Login({
  site,
  onLogin,
}: {
  site: Settings;
  onLogin: (user: User) => void;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const submit = async (values: Data) => {
    setLoading(true);
    setError("");
    try {
      onLogin(await api<User>("/auth/login", "POST", values));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setLoading(false);
    }
  };
  return (
    <div className="login-page">
      <section className="login-intro">
        <div className="login-brand">
          <CheckSquareOutlined /> TICKET HUB
        </div>
        <div>
          <span className="eyebrow">让每一张工单，都被及时看见</span>
          <h1>
            同步工作进展
            <br />
            不错过重要提醒。
          </h1>
          <p>
            汇聚工单、实时消息与团队提醒，
            <br />
            在一个安全、清晰的工作空间中掌握全局。
          </p>
          <div className="login-features">
            <span>
              <CheckCircleOutlined /> 多类型工单同步
            </span>
            <span>
              <BellOutlined /> 实时提醒
            </span>
            <span>
              <LockOutlined /> 只读访问上游
            </span>
          </div>
        </div>
        <small>工单同步与提醒 · 不认领、不处理、不回写</small>
      </section>
      <section className="login-form">
        <div className="login-box">
          {site.logo_url && (
            <img src={site.logo_url} className="login-logo" alt="站点标志" />
          )}
          <span className="eyebrow">欢迎回来</span>
          <h2>登录{site.site_name}</h2>
          <p className="muted">使用您的工作账号进入工作空间</p>
          <Failure error={error} />
          <Form layout="vertical" onFinish={submit} requiredMark={false}>
            <Form.Item
              name="username"
              label="用户名"
              rules={[{ required: true, message: "请输入用户名" }]}
            >
              <Input
                prefix={<UserOutlined />}
                size="large"
                autoComplete="username"
                placeholder="请输入用户名"
              />
            </Form.Item>
            <Form.Item
              name="password"
              label="密码"
              rules={[{ required: true, message: "请输入密码" }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                size="large"
                autoComplete="current-password"
                placeholder="请输入密码"
              />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={loading}
            >
              登录工作空间 <ArrowRightOutlined />
            </Button>
          </Form>
          <p className="login-help">无法登录？请联系站点管理员</p>
        </div>
      </section>
    </div>
  );
}

export function Tickets() {
  const { catalog } = useHub();
  const mobile = useMobile();
  const [filters, setFilters] = useState<Data>({});
  const [page, setPage] = useState(1);
  const [rows, setRows] = useState<Ticket[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [form] = Form.useForm();
  const sentinel = useRef<HTMLDivElement>(null);
  const filterKey = JSON.stringify(filters);
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api<Page<Ticket>>(
      `/tickets?${query({ ...JSON.parse(filterKey), page, page_size: 20 })}`,
    )
      .then((data) => {
        if (active) {
          setRows((old) =>
            mobile && page > 1 ? [...old, ...data.items] : data.items || [],
          );
          setTotal(data.total);
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
  }, [filterKey, page, reload, mobile]);
  useEffect(() => {
    if (!mobile || loading || error || rows.length >= total) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) setPage((v) => v + 1);
      },
      { rootMargin: "100px" },
    );
    if (sentinel.current) observer.observe(sentinel.current);
    return () => observer.disconnect();
  }, [mobile, loading, error, rows.length, total]);
  const apply = (values: Data) => {
    const { range, ...rest } = values;
    setPage(1);
    setFilters({
      ...rest,
      from: range?.[0]?.startOf("day").toISOString(),
      to: range?.[1]?.endOf("day").toISOString(),
    });
  };
  const kindName = (v: string) =>
    catalog.kinds.find((k) => k.value === v)?.label || v;
  return (
    <>
      <PageHead
        title="工单中心"
        description="集中查看上游工单与处理进展，所有数据仅同步、不回写。"
        extra={
          <Refresh
            onClick={() => {
              setPage(1);
              setReload((v) => v + 1);
            }}
            loading={loading}
          />
        }
      />
      <div className="summary-strip">
        <div>
          <span className="metric-icon">
            <CheckSquareOutlined />
          </span>
          <div>
            <small>筛选工单总数</small>
            <strong>
              {total}
              <em>张</em>
            </strong>
          </div>
        </div>
        <div>
          <span className="metric-icon amber">
            <BellOutlined />
          </span>
          <div>
            <small>本页待认领</small>
            <strong>
              {rows.filter((r) => r.status === "待认领").length}
              <em>张</em>
            </strong>
          </div>
        </div>
        <div className="summary-note">
          <span className="live-dot online" /> 上游只读同步{" "}
          <small>自动采集 · 安全可追溯</small>
        </div>
      </div>
      <Card className="filter-card">
        <div className="section-caption">
          <FilterOutlined /> 筛选工单
        </div>
        <Form form={form} onFinish={apply} className="filter-form">
          <Form.Item name="keyword">
            <Input
              allowClear
              prefix={<SearchOutlined />}
              placeholder="搜索标题或工单编号"
              aria-label="搜索工单"
            />
          </Form.Item>
          <Form.Item name="kind">
            <Select
              allowClear
              placeholder="全部类型"
              options={catalog.kinds}
              aria-label="工单类型"
            />
          </Form.Item>
          <Form.Item name="status">
            <Select
              allowClear
              placeholder="全部状态"
              options={catalog.statuses.map((v) => ({ value: v, label: v }))}
              aria-label="工单状态"
            />
          </Form.Item>
          <Form.Item name="site">
            <Select
              showSearch
              allowClear
              placeholder="全部机房"
              options={catalog.sites.map((v) => ({ value: v, label: v }))}
              aria-label="机房"
            />
          </Form.Item>
          <Form.Item name="range">
            <DatePicker.RangePicker aria-label="创建时间范围" />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>
              查询
            </Button>
            <Button
              onClick={() => {
                form.resetFields();
                apply({});
              }}
            >
              重置
            </Button>
          </Space>
        </Form>
      </Card>
      <Failure error={error} retry={() => setReload((v) => v + 1)} />
      <Card
        className="list-panel"
        title={
          <span>
            全部工单 <span className="count-pill">{total}</span>
          </span>
        }
        extra={<span className="muted small">按同步时间倒序</span>}
      >
        {mobile ? (
          <>
            <div className="ticket-cards">
              {rows.map((t) => (
                <Link
                  to={`/tickets/${t.id}`}
                  key={t.id}
                  className="ticket-card"
                >
                  <div className="card-top">
                    <span className="mono muted">{t.upstream_id}</span>
                    <Status value={t.status} />
                  </div>
                  <h3>{t.title}</h3>
                  <div className="ticket-card-meta">
                    <Tag>{kindName(t.kind)}</Tag>
                    <span>{t.site || "未指定机房"}</span>
                  </div>
                  <div className="card-bottom">
                    <span>更新于 {dateText(t.updated_at)}</span>
                    <ArrowRightOutlined />
                  </div>
                </Link>
              ))}
            </div>
            {rows.length === 0 && <Blank loading={loading} />}
            <div ref={sentinel} className="load-more">
              {loading ? (
                "正在加载…"
              ) : rows.length < total ? (
                <Button onClick={() => setPage((v) => v + 1)}>加载更多</Button>
              ) : rows.length > 0 ? (
                "已显示全部工单"
              ) : null}
            </div>
          </>
        ) : (
          <Table<Ticket>
            rowKey="id"
            dataSource={rows}
            loading={loading}
            pagination={{
              current: page,
              pageSize: 20,
              total,
              showSizeChanger: false,
              showTotal: (t) => `共 ${t} 张工单`,
              onChange: setPage,
            }}
            columns={[
              {
                title: "工单",
                dataIndex: "title",
                width: "36%",
                render: (_, t) => (
                  <div>
                    <Link className="ticket-title" to={`/tickets/${t.id}`}>
                      {t.title}
                    </Link>
                    <div className="mono small muted">{t.upstream_id}</div>
                  </div>
                ),
              },
              {
                title: "类型",
                dataIndex: "kind",
                render: (v) => <Tag className="kind-tag">{kindName(v)}</Tag>,
              },
              {
                title: "状态",
                dataIndex: "status",
                render: (v) => <Status value={v} />,
              },
              { title: "机房", dataIndex: "site", render: (v) => v || "—" },
              {
                title: "处理人",
                dataIndex: "assignee",
                render: (v) => v || "未分配",
              },
              { title: "同步时间", dataIndex: "updated_at", render: dateText },
              {
                title: "",
                key: "detail",
                render: (_, t) => (
                  <Link to={`/tickets/${t.id}`}>
                    详情 <ArrowRightOutlined />
                  </Link>
                ),
              },
            ]}
          />
        )}
      </Card>
    </>
  );
}

export function TicketDetail() {
  const { id } = useParams();
  const { catalog } = useHub();
  const mobile = useMobile();
  const [data, setData] = useState<{ ticket: Ticket; events: TicketEvent[] }>();
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    setError("");
    api<{ ticket: Ticket; events: TicketEvent[] }>(`/tickets/${id}`)
      .then((v) => {
        if (active) setData(v);
      })
      .catch((e) => {
        if (active) setError(errorText(e));
      });
    return () => {
      active = false;
    };
  }, [id, reload]);
  if (!data)
    return (
      <>
        <Link to="/tickets">
          <ArrowLeftOutlined /> 返回工单列表
        </Link>
        <Failure error={error} retry={() => setReload((v) => v + 1)} />
        {!error && <Blank loading />}
      </>
    );
  const t = data.ticket;
  return (
    <>
      <Link className="back-link" to="/tickets">
        <ArrowLeftOutlined /> 返回工单列表
      </Link>
      <PageHead
        title={t.title}
        description={`${t.upstream_id} · ${catalog.kinds.find((k) => k.value === t.kind)?.label || t.kind}`}
        extra={<Status value={t.status} />}
      />
      <Failure error={error} />
      <div className="detail-grid">
        <Card title="工单信息">
          <Descriptions
            column={mobile ? 1 : 2}
            items={[
              ["工单编号", t.upstream_id],
              [
                "工单类型",
                catalog.kinds.find((k) => k.value === t.kind)?.label || t.kind,
              ],
              ["当前状态", <Status value={t.status} />],
              ["优先级", t.priority],
              ["机房 / 园区", t.site],
              ["当前节点", t.node],
              ["处理人", t.assignee],
              ["截止时间", dateText(t.deadline)],
              ["上游创建时间", dateText(t.created_at_upstream)],
              ["上游更新时间", dateText(t.updated_at_upstream)],
              ["首次同步时间", dateText(t.created_at)],
              ["最近同步时间", dateText(t.updated_at)],
            ].map(([label, children], i) => ({
              key: i,
              label,
              children: children || "—",
            }))}
          />
          <Alert
            className="readonly-note"
            type="info"
            showIcon
            message="这里仅展示同步信息，认领和处理请前往上游系统。"
          />
        </Card>
        <Card
          title={
            <span>
              事件时间线{" "}
              <span className="count-pill">{data.events.length}</span>
            </span>
          }
        >
          <Timeline
            items={[...data.events].reverse().map((ev) => ({
              color: ev.type === "ticket.created" ? "green" : "blue",
              children: (
                <div>
                  <strong>
                    {events.find((e) => e.value === ev.type)?.label || ev.type}
                  </strong>
                  <p className="muted small">{dateText(ev.created_at)}</p>
                  <Collapse
                    size="small"
                    ghost
                    items={[
                      {
                        key: ev.id,
                        label: "查看变更快照",
                        children: (
                          <pre>
                            {JSON.stringify(
                              { before: ev.before, after: ev.after },
                              null,
                              2,
                            )}
                          </pre>
                        ),
                      },
                    ]}
                  />
                </div>
              ),
            }))}
          />
        </Card>
      </div>
      <Card className="raw-panel">
        <Collapse
          ghost
          items={[
            {
              key: "raw",
              label: "原始 JSON · 调试与字段核对",
              children: <pre>{JSON.stringify(t.raw, null, 2)}</pre>,
            },
          ]}
        />
      </Card>
    </>
  );
}

export function Messages() {
  const { revision, refreshUnread } = useHub();
  const mobile = useMobile();
  const [page, setPage] = useState(1);
  const [read, setRead] = useState("");
  const [data, setData] = useState<Page<Notice>>({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [browser, setBrowser] = useState(
    localStorage.getItem("hub-browser-notifications") === "true",
  );
  useEffect(() => {
    let active = true;
    setLoading(true);
    api<Page<Notice>>(`/notifications?${query({ page, read })}`)
      .then((v) => {
        if (active) {
          setData({ ...v, items: v.items || [] });
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
  }, [page, read, revision, reload]);
  const mark = async (id?: number) => {
    try {
      await api(
        id ? `/notifications/${id}/read` : "/notifications/read-all",
        "PUT",
        {},
      );
      await refreshUnread();
      setReload((v) => v + 1);
    } catch (e) {
      message.error(errorText(e));
    }
  };
  const toggleBrowser = async () => {
    if (!("Notification" in window)) {
      message.warning("当前浏览器不支持系统通知");
      return;
    }
    const enabled = !browser;
    if (enabled && (await Notification.requestPermission()) !== "granted") {
      message.warning("浏览器未允许通知，请检查权限设置");
      return;
    }
    setBrowser(enabled);
    localStorage.setItem("hub-browser-notifications", String(enabled));
  };
  return (
    <>
      <PageHead
        title="消息中心"
        description="重要变化即刻送达，所有提醒都在这里。"
        extra={
          <Button icon={<CheckCircleOutlined />} onClick={() => void mark()}>
            全部标记已读
          </Button>
        }
      />
      <div className="message-toolbar">
        <Tabs
          activeKey={read}
          onChange={(v) => {
            setRead(v);
            setPage(1);
          }}
          items={[
            { key: "", label: "全部消息" },
            { key: "false", label: "未读消息" },
            { key: "true", label: "已读消息" },
          ]}
        />
        <Checkbox checked={browser} onChange={() => void toggleBrowser()}>
          浏览器通知
        </Checkbox>
      </div>
      <Failure error={error} retry={() => setReload((v) => v + 1)} />
      <div className="message-list">
        {data.items.map((n) => (
          <Card
            key={n.id}
            className={`message-card ${!n.read ? "unread" : ""}`}
          >
            <div className="message-icon">
              <BellOutlined />
            </div>
            <div className="message-body">
              <div className="card-top">
                <h3>
                  {!n.read && <Badge status="processing" />} {n.title}
                </h3>
                <span className="muted small">{dateText(n.created_at)}</span>
              </div>
              <p>{n.content}</p>
              <Space>
                {n.ticket_id > 0 && (
                  <Link
                    to={`/tickets/${n.ticket_id}`}
                    onClick={() => {
                      if (!n.read) void mark(n.id);
                    }}
                  >
                    查看工单 <ArrowRightOutlined />
                  </Link>
                )}
                {!n.read && (
                  <Button
                    size="small"
                    type="text"
                    onClick={() => void mark(n.id)}
                  >
                    标记已读
                  </Button>
                )}
              </Space>
            </div>
          </Card>
        ))}
        {!data.items.length && (
          <Blank
            loading={loading}
            children={<Empty description="暂时没有消息" />}
          />
        )}
      </div>
      <Pagination
        className="pagination"
        simple={mobile}
        current={page}
        pageSize={20}
        total={data.total}
        onChange={setPage}
        showSizeChanger={false}
      />
    </>
  );
}

export function Profile() {
  const { user, setUser, catalog } = useHub();
  const [form] = Form.useForm();
  const [profile] = Form.useForm();
  const [password] = Form.useForm();
  const [available, setAvailable] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    api<{ subscription: Data; available_channels: string[] }>(
      "/me/subscription",
    )
      .then((v) => {
        if (active) {
          form.setFieldsValue({
            ...v.subscription,
            kinds: v.subscription.kinds || [],
            sites: v.subscription.sites || [],
            event_types: v.subscription.event_types || [],
            channels: (v.subscription.channels || []).filter((c: string) =>
              v.available_channels.includes(c),
            ),
          });
          setAvailable(v.available_channels);
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
  const save = async (kind: string, values: Data) => {
    setBusy(kind);
    try {
      if (kind === "profile") {
        const u = await api<User>("/me/profile", "PUT", values);
        setUser(u);
      } else if (kind === "password") {
        await api("/auth/password", "PUT", values);
        setUser(null);
      } else {
        await api("/me/subscription", "PUT", values);
      }
      message.success("保存成功");
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setBusy("");
    }
  };
  return (
    <>
      <PageHead
        title="我的工作台"
        description="完善联系方式，定制适合您的提醒方式。"
      />
      <div className="profile-banner">
        <Avatar size={56} style={{ background: "#dcebe2", color: "#155e55" }}>
          {(user.display_name || user.username).slice(0, 1)}
        </Avatar>
        <div>
          <h2>{user.display_name || user.username}</h2>
          <span className="muted">
            {user.username}{" "}
            <Tag>{user.role === "admin" ? "管理员" : "团队成员"}</Tag>
          </span>
        </div>
      </div>
      <Tabs
        defaultActiveKey="subscription"
        items={[
          {
            key: "subscription",
            label: "提醒订阅",
            children: (
              <Card title="我的提醒偏好" className="form-card">
                <Failure error={error} retry={() => setReload((v) => v + 1)} />
                {loading ? (
                  <Blank loading />
                ) : (
                  <Form
                    form={form}
                    layout="vertical"
                    onFinish={(v) => void save("subscription", v)}
                  >
                    <Form.Item
                      name="enabled"
                      label="启用个人订阅"
                      valuePropName="checked"
                    >
                      <Switch
                        checkedChildren="已开启"
                        unCheckedChildren="已关闭"
                      />
                    </Form.Item>
                    <div className="form-grid">
                      <Form.Item
                        name="kinds"
                        label="关注的工单类型"
                        extra="留空表示全部类型"
                      >
                        <Select
                          mode="multiple"
                          options={catalog.kinds}
                          placeholder="全部工单类型"
                        />
                      </Form.Item>
                      <Form.Item
                        name="sites"
                        label="关注的机房"
                        extra="输入机房名称后按回车，留空表示全部"
                      >
                        <Select
                          mode="tags"
                          options={catalog.sites.map((v) => ({
                            label: v,
                            value: v,
                          }))}
                          placeholder="全部机房"
                        />
                      </Form.Item>
                      <Form.Item name="event_types" label="事件类型">
                        <Select
                          mode="multiple"
                          options={events}
                          placeholder="全部事件"
                        />
                      </Form.Item>
                      <Form.Item
                        name="channels"
                        label="提醒渠道"
                        extra="仅显示管理员已启用的个人渠道；站内消息始终兜底"
                      >
                        <Checkbox.Group
                          options={available.map((v) => ({
                            label: channelNames[v],
                            value: v,
                          }))}
                        />
                      </Form.Item>
                    </div>
                    <Divider orientation="left">免打扰设置</Divider>
                    <div className="form-grid">
                      <Form.Item
                        name="quiet_start"
                        label="开始时间"
                        rules={[
                          {
                            pattern: /^$|^([01]\d|2[0-3]):[0-5]\d$/,
                            message: "请输入 HH:MM",
                          },
                        ]}
                      >
                        <Input placeholder="例如 22:00" />
                      </Form.Item>
                      <Form.Item
                        name="quiet_end"
                        label="结束时间"
                        rules={[
                          {
                            pattern: /^$|^([01]\d|2[0-3]):[0-5]\d$/,
                            message: "请输入 HH:MM",
                          },
                        ]}
                      >
                        <Input placeholder="例如 08:00" />
                      </Form.Item>
                      <Form.Item name="timezone" label="时区">
                        <Input placeholder="Asia/Shanghai" />
                      </Form.Item>
                    </div>
                    <p className="muted small">
                      支持跨午夜；开始和结束需同时填写，相同时间表示全天免打扰。
                    </p>
                    <Form.Item name="urgent_bypass" valuePropName="checked">
                      <Checkbox>紧急工单突破免打扰</Checkbox>
                    </Form.Item>
                    <Form.Item name="mention_in_group" valuePropName="checked">
                      <Checkbox>关注的消息在群里 @我</Checkbox>
                    </Form.Item>
                    <Button
                      type="primary"
                      htmlType="submit"
                      loading={busy === "subscription"}
                    >
                      保存订阅设置
                    </Button>
                  </Form>
                )}
              </Card>
            ),
          },
          {
            key: "profile",
            label: "个人资料",
            children: (
              <Card title="联系方式" className="form-card">
                <Form
                  form={profile}
                  layout="vertical"
                  initialValues={user}
                  onFinish={(v) => void save("profile", v)}
                >
                  <div className="form-grid">
                    <Form.Item
                      name="phone"
                      label="手机号"
                      rules={[
                        {
                          pattern: /^\+?\d*$/,
                          message: "只允许数字及前导加号",
                        },
                      ]}
                    >
                      <Input placeholder="用于短信提醒与企微 @" />
                    </Form.Item>
                    <Form.Item
                      name="email"
                      label="邮箱"
                      rules={[{ type: "email", message: "请输入合法邮箱" }]}
                    >
                      <Input placeholder="用于邮件提醒" />
                    </Form.Item>
                    <Form.Item name="feishu_open_id" label="飞书 Open ID">
                      <Input placeholder="可选，用于群内 @" />
                    </Form.Item>
                  </div>
                  <Button
                    type="primary"
                    htmlType="submit"
                    loading={busy === "profile"}
                  >
                    保存资料
                  </Button>
                </Form>
              </Card>
            ),
          },
          {
            key: "password",
            label: "账号安全",
            children: (
              <Card title="修改登录密码" className="form-card">
                <Alert
                  showIcon
                  type="info"
                  message="密码修改后需要重新登录。"
                />
                <Form
                  form={password}
                  layout="vertical"
                  onFinish={(v) => void save("password", v)}
                >
                  <Form.Item
                    name="old_password"
                    label="当前密码"
                    rules={[{ required: true, message: "请输入当前密码" }]}
                  >
                    <Input.Password autoComplete="current-password" />
                  </Form.Item>
                  <Form.Item
                    name="new_password"
                    label="新密码"
                    rules={[
                      {
                        required: true,
                        min: 12,
                        max: 72,
                        message: "密码为 12~72 字节",
                      },
                    ]}
                  >
                    <Input.Password autoComplete="new-password" />
                  </Form.Item>
                  <Button
                    type="primary"
                    htmlType="submit"
                    loading={busy === "password"}
                  >
                    修改密码并重新登录
                  </Button>
                </Form>
              </Card>
            ),
          },
        ]}
      />
    </>
  );
}
