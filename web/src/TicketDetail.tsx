import { ArrowLeftOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Card from "antd/es/card";
import Collapse from "antd/es/collapse";
import Descriptions from "antd/es/descriptions";
import Timeline from "antd/es/timeline";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  api,
  dateText,
  errorText,
  events,
  type Ticket,
  type TicketEvent,
} from "./api";
import { Blank, Failure, PageHead, Status, useMobile } from "./common";
import { useHub } from "./context";

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
