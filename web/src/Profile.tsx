import Alert from "antd/es/alert";
import Avatar from "antd/es/avatar";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Checkbox from "antd/es/checkbox";
import Divider from "antd/es/divider";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Switch from "antd/es/switch";
import Tabs from "antd/es/tabs";
import Tag from "antd/es/tag";
import message from "antd/es/message";
import { useEffect, useState } from "react";
import {
  api,
  channelNames,
  errorText,
  events,
  type Data,
  type User,
} from "./api";
import { Blank, Failure, PageHead } from "./common";
import { useHub } from "./context";

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
