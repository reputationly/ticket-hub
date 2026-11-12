import {
  ArrowRightOutlined,
  BellOutlined,
  CheckCircleOutlined,
} from "@ant-design/icons";
import Badge from "antd/es/badge";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Checkbox from "antd/es/checkbox";
import Empty from "antd/es/empty";
import Pagination from "antd/es/pagination";
import Space from "antd/es/space";
import Tabs from "antd/es/tabs";
import message from "antd/es/message";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, dateText, errorText, query, type Notice, type Page } from "./api";
import { Blank, Failure, PageHead, useMobile } from "./common";
import { useHub } from "./context";

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
