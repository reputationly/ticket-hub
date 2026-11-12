import { useEffect, useState, type ReactNode } from "react";
import { Alert, Button, Empty, Spin, Tag } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
export function useMobile() {
  const [mobile, setMobile] = useState(window.innerWidth < 768);
  useEffect(() => {
    const media = window.matchMedia("(max-width: 767px)");
    const update = () => setMobile(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return mobile;
}
export function Status({ value }: { value: unknown }) {
  const text = String(value || "—");
  const color = /失败|过期|error|failed/.test(text)
    ? "red"
    : /在线|已完成|sent|success/.test(text)
      ? "green"
      : /待|pending/.test(text)
        ? "gold"
        : /处理中|processing|登录中|等待/.test(text)
          ? "blue"
          : undefined;
  const names: Record<string, string> = {
    pending: "待发送",
    processing: "处理中",
    sent: "已送达",
    failed: "发送失败",
  };
  return <Tag color={color}>{names[text] || text}</Tag>;
}
export function PageHead({
  title,
  description,
  extra,
}: {
  title: string;
  description?: string;
  extra?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
        <div className="eyebrow">TICKET HUB / 工作空间</div>
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      <div className="head-actions">{extra}</div>
    </div>
  );
}
export function Failure({
  error,
  retry,
}: {
  error: string;
  retry?: () => void;
}) {
  return error ? (
    <Alert
      className="error-banner"
      type="error"
      showIcon
      message={error}
      action={
        retry && (
          <Button size="small" onClick={retry}>
            重试
          </Button>
        )
      }
    />
  ) : null;
}
export function Refresh({
  onClick,
  loading,
}: {
  onClick: () => void;
  loading?: boolean;
}) {
  return (
    <Button icon={<ReloadOutlined />} onClick={onClick} loading={loading}>
      刷新
    </Button>
  );
}
export function Blank({
  loading,
  children,
}: {
  loading: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="blank">
      {loading ? <Spin /> : children || <Empty description="暂无记录" />}
    </div>
  );
}
