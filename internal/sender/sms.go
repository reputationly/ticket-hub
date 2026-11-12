package sender

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysms "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/reputationly/ticket-hub/internal/model"
)

type SMS struct {
	Client  *http.Client
	BaseURL string
}
type sdkHTTP struct {
	ctx    context.Context
	client *http.Client
}

func (s sdkHTTP) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
	return s.client.Do(req.WithContext(s.ctx))
}
func (s *SMS) Send(ctx context.Context, ch model.Channel, d model.Delivery, _ uint) error {
	if d.Phone == "" {
		return errors.New("短信缺少手机号")
	}
	endpoint, protocol := "dysmsapi.aliyuncs.com", "https"
	if s.BaseURL != "" {
		u, e := url.Parse(s.BaseURL)
		if e != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("短信地址配置错误")
		}
		endpoint, protocol = u.Host, u.Scheme
	}
	client, e := dysms.NewClient(&openapi.Config{AccessKeyId: tea.String(Text(ch.Config, "access_key_id")), AccessKeySecret: tea.String(Text(ch.Config, "access_key_secret")), Endpoint: tea.String(endpoint), Protocol: tea.String(protocol), HttpClient: sdkHTTP{ctx, s.Client}})
	if e != nil {
		return errors.New("短信客户端初始化失败")
	}
	params := map[string]string{}
	if mappings, ok := ch.Config["template_params"].(map[string]any); ok {
		for name, field := range mappings {
			key, ok := field.(string)
			if !ok {
				return errors.New("短信模板映射格式错误")
			}
			v, ok := d.Variables[key]
			if !ok {
				return errors.New("短信模板字段不存在")
			}
			params[name] = v
		}
	}
	data, e := json.Marshal(params)
	if e != nil {
		return e
	}
	result, e := client.SendSmsWithOptions(&dysms.SendSmsRequest{PhoneNumbers: tea.String(d.Phone), SignName: tea.String(Text(ch.Config, "sign_name")), TemplateCode: tea.String(Text(ch.Config, "template_code")), TemplateParam: tea.String(string(data))}, &util.RuntimeOptions{Autoretry: tea.Bool(false), ConnectTimeout: tea.Int(10000), ReadTimeout: tea.Int(15000)})
	if e != nil {
		return errors.New("阿里云短信请求失败")
	}
	if result == nil || result.Body == nil || tea.StringValue(result.Body.Code) != "OK" {
		code := "unknown"
		if result != nil && result.Body != nil {
			code = tea.StringValue(result.Body.Code)
		}
		if strings.ContainsAny(code, "\r\n") || len(code) > 100 {
			code = "unknown"
		}
		return errors.New("阿里云短信返回错误码：" + code)
	}
	return nil
}
