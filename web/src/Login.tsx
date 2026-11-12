import {
  ArrowRightOutlined,
  BellOutlined,
  CheckCircleOutlined,
  CheckSquareOutlined,
  LockOutlined,
  UserOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import Form from "antd/es/form";
import Input from "antd/es/input";
import { useState } from "react";
import { api, errorText, type Data, type Settings, type User } from "./api";
import { Failure } from "./common";

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
