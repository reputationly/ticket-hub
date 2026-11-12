package sender

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/model"
)

func fakeSMTP(t *testing.T, mode string, reject bool) (string, *tls.Config, <-chan string) {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	parsed, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if mode == "ssl" {
			conn = tls.Server(conn, serverTLS)
		}
		reader := bufio.NewReader(conn)
		_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
		for {
			line, e := reader.ReadString('\n')
			if e != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				if mode == "starttls" {
					_, _ = fmt.Fprint(conn, "250-local\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
				} else {
					_, _ = fmt.Fprint(conn, "250-local\r\n250 AUTH PLAIN\r\n")
				}
			case strings.HasPrefix(line, "HELO"):
				_, _ = fmt.Fprint(conn, "250 local\r\n")
			case strings.HasPrefix(line, "STARTTLS"):
				_, _ = fmt.Fprint(conn, "220 ready\r\n")
				secure := tls.Server(conn, serverTLS)
				if e = secure.Handshake(); e != nil {
					return
				}
				conn = secure
				reader = bufio.NewReader(conn)
			case strings.HasPrefix(line, "AUTH"):
				if reject {
					_, _ = fmt.Fprint(conn, "535 auth rejected\r\n")
				} else {
					_, _ = fmt.Fprint(conn, "235 auth ok\r\n")
				}
			case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
				if reject {
					_, _ = fmt.Fprint(conn, "550 rejected\r\n")
				} else {
					_, _ = fmt.Fprint(conn, "250 ok\r\n")
				}
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 data\r\n")
				var data strings.Builder
				for {
					line, e = reader.ReadString('\n')
					if e != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					data.WriteString(line)
				}
				messages <- data.String()
				_, _ = fmt.Fprint(conn, "250 queued\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	return listener.Addr().String(), clientTLS, messages
}
func TestSMTP(t *testing.T) {
	for _, mode := range []string{"none", "ssl", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			addr, tc, messages := fakeSMTP(t, mode, false)
			host, port, _ := net.SplitHostPort(addr)
			cfg := map[string]any{"host": host, "port": port, "from": "工单中心 <from@example.com>", "tls": mode}
			if mode != "none" {
				cfg["username"] = "local-user"
				cfg["password"] = "local-password"
			}
			s := &SMTP{tc}
			if e := s.Send(context.Background(), model.Channel{Config: cfg}, model.Delivery{Email: "to@example.com", Title: "工单提醒", Content: "<script>不能执行</script>"}, 0); e != nil {
				t.Fatal(e)
			}
			select {
			case data := <-messages:
				if !strings.Contains(data, "To: <to@example.com>") || !strings.Contains(data, "Content-Type: text/html") {
					t.Fatal(data)
				}
				parts := strings.SplitN(data, "\r\n\r\n", 2)
				if len(parts) != 2 {
					t.Fatal(data)
				}
				decoded, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(parts[1])))
				if e != nil || !strings.Contains(string(decoded), "&lt;script&gt;") {
					t.Fatal(string(decoded), e)
				}
			case <-time.After(time.Second):
				t.Fatal("未收到邮件")
			}
		})
	}
	t.Run("拒绝", func(t *testing.T) {
		addr, tc, _ := fakeSMTP(t, "none", true)
		host, port, _ := net.SplitHostPort(addr)
		s := &SMTP{tc}
		if e := s.Send(context.Background(), model.Channel{Config: map[string]any{"host": host, "port": port, "from": "from@example.com", "tls": "none"}}, model.Delivery{Email: "to@example.com"}, 0); e == nil {
			t.Fatal("SMTP 拒绝未传播")
		}
	})
	s := &SMTP{}
	if e := s.Send(context.Background(), model.Channel{}, model.Delivery{}, 0); e == nil {
		t.Fatal("空地址未拒绝")
	}
	if e := s.Send(context.Background(), model.Channel{Config: map[string]any{"from": "from@example.com", "html_template": "{{.Missing}}"}}, model.Delivery{Email: "to@example.com"}, 0); e == nil {
		t.Fatal("模板错误未拒绝")
	}
}
