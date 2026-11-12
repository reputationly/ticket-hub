package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

type Cipher struct{ aead cipher.AEAD }

func New(secret string) (*Cipher, error) {
	if len(secret) < 32 {
		return nil, errors.New("加密主密钥至少需要 32 字节")
	}
	key := sha256.Sum256([]byte("ticket-hub:encryption:v1:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(plaintext), []byte("ticket-hub:v1"))), nil
}

func (c *Cipher) Decrypt(encoded string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(data) < c.aead.NonceSize() {
		return "", errors.New("密文格式错误")
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, data[:n], data[n:], []byte("ticket-hub:v1"))
	if err != nil {
		return "", errors.New("密文校验失败")
	}
	return string(plain), nil
}
