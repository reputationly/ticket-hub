package crypto

import (
	"strings"
	"testing"
)

func TestCipher(t *testing.T) {
	c, err := New(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "渠道密钥测试"} {
		encoded, err := c.Encrypt(value)
		if err != nil {
			t.Fatal(err)
		}
		other, err := c.Encrypt(value)
		if err != nil || other == encoded {
			t.Fatal("随机 nonce 未生效")
		}
		plain, err := c.Decrypt(encoded)
		if err != nil || plain != value {
			t.Fatal(plain, err)
		}
		wrong, _ := New(strings.Repeat("b", 32))
		if _, err = wrong.Decrypt(encoded); err == nil {
			t.Fatal("错误密钥解密成功")
		}
		if _, err = c.Decrypt(encoded[:len(encoded)-2]); err == nil {
			t.Fatal("篡改密文解密成功")
		}
	}
	if _, err = c.Decrypt("!"); err == nil {
		t.Fatal("无效密文被接受")
	}
	if _, err = New("short"); err == nil {
		t.Fatal("弱密钥被接受")
	}
}
