import React, { useCallback, useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import {
  App as AntApp,
  Avatar,
  Badge,
  Button,
  ConfigProvider,
  Dropdown,
  Menu,
  Result,
  Spin,
  Tooltip,
  message,
} from "antd";
import zhCN from "antd/locale/zh_CN";
import {
  BellOutlined,
  CheckSquareOutlined,
  ControlOutlined,
  LogoutOutlined,
  MenuOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  UserOutlined,
} from "@ant-design/icons";
import {
  BrowserRouter,
  Link,
  Navigate,
  Outlet,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  api,
  errorText,
  type Catalog,
  type Notice,
  type Settings,
  type User,
} from "./api";
import { Hub, useHub } from "./context";
import { useMobile } from "./common";
import { Login, Messages, Profile, TicketDetail, Tickets } from "./pages";
import { AdminMenu, ResourcePage, SiteSettings } from "./management";
import { resources } from "./schemas";
import "dayjs/locale/zh-cn";
import "./style.css";

function Application() {
  const navigate = useNavigate();
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);
  const [site, setSite] = useState<Settings>({
    site_name: "工单中心",
    logo_url: "",
  });
  const [catalog, setCatalog] = useState<Catalog>({
    kinds: [],
    sites: [],
    statuses: [],
  });
  const [unread, setUnread] = useState(0);
  const [revision, setRevision] = useState(0);
  const [live, setLive] = useState(false);
  const reloadSite = useCallback(async () => {
    const v = await api<Settings>("/site");
    setSite(v);
    document.title = v.site_name;
  }, []);
  const refreshUnread = useCallback(async () => {
    const v = await api<{ count: number }>("/notifications/unread-count");
    setUnread(v.count);
  }, []);
  useEffect(() => {
    void reloadSite().catch(() => {});
    void api<User>("/auth/me")
      .then(setUser)
      .catch(() => {})
      .finally(() => setReady(true));
    const expired = () => {
      setUser(null);
      navigate("/login", { replace: true });
    };
    window.addEventListener("session-expired", expired);
    return () => window.removeEventListener("session-expired", expired);
  }, [navigate, reloadSite]);
  useEffect(() => {
    if (!user) return;
    let active = true;
    void api<Catalog>("/catalog")
      .then((v) => {
        if (active) setCatalog(v);
      })
      .catch((e) => message.error(errorText(e)));
    void refreshUnread().catch(() => {});
    const stream = new EventSource("/api/notifications/stream", {
      withCredentials: true,
    });
    stream.onopen = () => {
      setLive(true);
      void refreshUnread().catch(() => {});
      setRevision((v) => v + 1);
    };
    stream.onerror = () => {
      setLive(false);
      void api("/auth/me").catch(() => {});
    };
    stream.addEventListener("notification", (event) => {
      try {
        const n = JSON.parse((event as MessageEvent).data) as Notice;
        setRevision((v) => v + 1);
        void refreshUnread().catch(() => {});
        message.info(n.title);
        if (
          localStorage.getItem("hub-browser-notifications") === "true" &&
          "Notification" in window &&
          Notification.permission === "granted"
        ) {
          const notification = new Notification(n.title, { body: n.content });
          notification.onclick = () => {
            window.focus();
            navigate(n.ticket_id ? `/tickets/${n.ticket_id}` : "/messages");
          };
        }
      } catch {
        /* 无效事件由重连校准。 */
      }
    });
    return () => {
      active = false;
      stream.close();
      setLive(false);
    };
  }, [user?.id, refreshUnread, navigate]);
  if (!ready)
    return (
      <div className="boot">
        <Spin size="large" />
        <p>正在连接工作空间…</p>
      </div>
    );
  return (
    <Routes>
      <Route
        path="/login"
        element={
          user ? (
            <Navigate to="/tickets" replace />
          ) : (
            <Login site={site} onLogin={setUser} />
          )
        }
      />
      <Route
        element={
          user ? (
            <Hub.Provider
              value={{
                user,
                setUser,
                site,
                reloadSite,
                catalog,
                unread,
                refreshUnread,
                revision,
                live,
              }}
            >
              <Shell />
            </Hub.Provider>
          ) : (
            <Navigate to="/login" replace />
          )
        }
      >
        <Route index element={<Navigate to="/tickets" replace />} />
        <Route path="tickets" element={<Tickets />} />
        <Route path="tickets/:id" element={<TicketDetail />} />
        <Route path="messages" element={<Messages />} />
        <Route path="me" element={<Profile />} />
        <Route
          element={
            user?.role === "admin" ? (
              <Outlet />
            ) : (
              <Result status="403" title="仅管理员可访问" />
            )
          }
        >
          <Route path="admin" element={<AdminMenu />} />
          <Route path="admin/settings" element={<SiteSettings />} />
          {resources.map((resource) => (
            <Route
              key={resource.key}
              path={`admin/${resource.key}`}
              element={<ResourcePage key={resource.key} resource={resource} />}
            />
          ))}
        </Route>
        <Route
          path="*"
          element={
            <Result
              status="404"
              title="页面不存在"
              extra={<Link to="/tickets">返回工单</Link>}
            />
          }
        />
      </Route>
    </Routes>
  );
}
function Brand() {
  const { site } = useHub();
  return (
    <Link to="/tickets" className="brand">
      {site.logo_url ? (
        <img src={site.logo_url} alt="站点标志" />
      ) : (
        <span className="brand-symbol">
          <CheckSquareOutlined />
        </span>
      )}
      <span>
        {site.site_name}
        <small>运维协同 · 只读同步</small>
      </span>
    </Link>
  );
}
function Shell() {
  const { user, setUser, unread, live } = useHub();
  const mobile = useMobile();
  const location = useLocation();
  const navigate = useNavigate();
  const current = location.pathname.startsWith("/admin")
    ? "/admin"
    : location.pathname.startsWith("/tickets")
      ? "/tickets"
      : location.pathname;
  const nav = [
    { key: "/tickets", icon: <CheckSquareOutlined />, label: "工单" },
    {
      key: "/messages",
      icon: (
        <Badge count={unread} size="small">
          <BellOutlined />
        </Badge>
      ),
      label: "消息",
    },
    { key: "/me", icon: <UserOutlined />, label: "我的" },
    ...(user.role === "admin"
      ? [{ key: "/admin", icon: <ControlOutlined />, label: "管理" }]
      : []),
  ];
  const logout = async () => {
    try {
      await api("/auth/logout", "POST");
      setUser(null);
      navigate("/login");
    } catch (e) {
      message.error(errorText(e));
    }
  };
  return (
    <div className="app-shell">
      {!mobile && (
        <aside className="sidebar">
          <Brand />
          <div className="nav-caption">工作台</div>
          <Menu
            mode="inline"
            selectedKeys={[current]}
            items={nav}
            onClick={({ key }) => navigate(key)}
          />
          {user.role === "admin" && (
            <>
              <div className="nav-caption">管理控制台</div>
              <Menu
                mode="inline"
                selectedKeys={[location.pathname]}
                items={[
                  {
                    key: "/admin/settings",
                    icon: <SettingOutlined />,
                    label: "站点设置",
                  },
                  ...resources.map((r) => ({
                    key: `/admin/${r.key}`,
                    icon: r.icon,
                    label: r.shortTitle || r.title,
                  })),
                ]}
                onClick={({ key }) => navigate(key)}
              />
            </>
          )}
          <div className="sidebar-note">
            <SafetyCertificateOutlined /> 安全同步，不回写上游
            <small>您的操作不会改变上游工单</small>
          </div>
        </aside>
      )}
      <div className="workspace">
        <header className="topbar">
          {mobile ? (
            <Brand />
          ) : (
            <span className="breadcrumb">
              工作空间 <span>/</span>{" "}
              {current === "/admin"
                ? "管理控制台"
                : nav.find((n) => n.key === current)?.label}
            </span>
          )}
          <div className="topbar-right">
            <Tooltip title={live ? "站内消息实时连接中" : "实时连接重试中"}>
              <span className={`live-dot ${live ? "online" : ""}`} />
              {!mobile && (
                <span className="muted">{live ? "实时连接" : "重连中"}</span>
              )}
            </Tooltip>
            {!mobile && (
              <Link
                className="notification-link"
                aria-label="消息"
                to="/messages"
              >
                <Badge count={unread} size="small">
                  <BellOutlined />
                </Badge>
              </Link>
            )}
            <Dropdown
              menu={{
                items: [
                  { key: "me", label: "我的资料", icon: <UserOutlined /> },
                  {
                    key: "logout",
                    label: "退出登录",
                    icon: <LogoutOutlined />,
                    danger: true,
                  },
                ],
                onClick: ({ key }) =>
                  key === "logout" ? void logout() : navigate("/me"),
              }}
            >
              <Button type="text" className="user-button">
                <Avatar
                  size={30}
                  style={{ background: "#e3eee8", color: "#155e55" }}
                >
                  {(user.display_name || user.username).slice(0, 1)}
                </Avatar>
                {!mobile && <span>{user.display_name || user.username}</span>}
              </Button>
            </Dropdown>
          </div>
        </header>
        <main className="content">
          <Outlet />
        </main>
        <footer className="desktop-footer">
          {new Date().getFullYear()} · 工单同步与提醒工作空间{" "}
          <span>仅同步 · 不回写</span>
        </footer>
      </div>
      {mobile && (
        <nav className="bottom-tabs" aria-label="主导航">
          {nav.map((n) => (
            <Link
              key={n.key}
              to={n.key}
              className={current === n.key ? "active" : ""}
            >
              {n.key === "/admin" ? <MenuOutlined /> : n.icon}
              <span>{n.label}</span>
            </Link>
          ))}
        </nav>
      )}
    </div>
  );
}
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#155e55",
          colorInfo: "#155e55",
          colorBgLayout: "#f6f7f9",
          borderRadius: 8,
          fontFamily:
            'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Button: { controlHeight: 36 },
          Table: { headerBg: "#f8faf9", headerColor: "#66756d" },
          Menu: { itemSelectedBg: "#eaf2ed", itemSelectedColor: "#155e55" },
        },
      }}
    >
      <AntApp>
        <BrowserRouter>
          <Application />
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>,
);
