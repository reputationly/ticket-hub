import {
  ArrowRightOutlined,
  BellOutlined,
  CheckSquareOutlined,
  FilterOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import Card from "antd/es/card";
import DatePicker from "antd/es/date-picker";
import Drawer from "antd/es/drawer";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Space from "antd/es/space";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  api,
  dateText,
  errorText,
  query,
  type Data,
  type Page,
  type Ticket,
} from "./api";
import { Blank, Failure, PageHead, Refresh, Status, useMobile } from "./common";
import { useHub } from "./context";

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
  const [filterOpen, setFilterOpen] = useState(false);
  const keyword = Form.useWatch("keyword", form) || "";
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
    setFilterOpen(false);
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
  const filterForm = (
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
  );
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
      {mobile ? (
        <>
          <div className="mobile-ticket-search">
            <Input.Search
              value={keyword}
              allowClear
              placeholder="搜索标题或工单编号"
              aria-label="搜索工单"
              onChange={(event) =>
                form.setFieldValue("keyword", event.target.value)
              }
              onSearch={() => apply(form.getFieldsValue())}
            />
            <Button
              icon={<FilterOutlined />}
              onClick={() => setFilterOpen(true)}
            >
              筛选
            </Button>
          </div>
          <Drawer
            title="筛选工单"
            placement="bottom"
            height="100%"
            open={filterOpen}
            onClose={() => setFilterOpen(false)}
            forceRender
          >
            {filterForm}
          </Drawer>
        </>
      ) : (
        <Card className="filter-card">
          <div className="section-caption">
            <FilterOutlined /> 筛选工单
          </div>
          {filterForm}
        </Card>
      )}
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
