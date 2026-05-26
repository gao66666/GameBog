package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// DecryptEventBody 解密事件订阅 encrypt 字段（AES-256-CBC）。
func DecryptEventBody(encryptKey, cipherB64 string) ([]byte, error) {
	encryptKey = strings.TrimSpace(encryptKey)
	cipherB64 = strings.TrimSpace(cipherB64)
	if encryptKey == "" || cipherB64 == "" {
		return nil, fmt.Errorf("empty encrypt payload")
	}
	raw, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return nil, err
	}
	if len(raw) < aes.BlockSize+1 {
		return nil, fmt.Errorf("cipher too short")
	}
	iv := raw[:aes.BlockSize]
	data := raw[aes.BlockSize:]
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(data))
	mode.CryptBlocks(plain, data)
	plain, err = pkcs7Unpad(plain)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty plaintext")
	}
	n := int(b[len(b)-1])
	if n <= 0 || n > len(b) {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := 0; i < n; i++ {
		if b[len(b)-1-i] != byte(n) {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return b[:len(b)-n], nil
}

// VerifySignature 校验 X-Lark-Signature（timestamp+nonce+encryptKey+body 的 SHA256 十六进制）。
func VerifySignature(timestamp, nonce, encryptKey, body, signature string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}
	var b strings.Builder
	b.WriteString(timestamp)
	b.WriteString(nonce)
	b.WriteString(encryptKey)
	b.WriteString(body)
	sum := sha256.Sum256([]byte(b.String()))
	expected := hex.EncodeToString(sum[:])
	return strings.EqualFold(expected, signature)
}

// ParseChallenge 解析 URL 校验请求。
func ParseChallenge(raw []byte, cfg Config) (challenge string, ok bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return "", false
	}
	var typ string
	if err := json.Unmarshal(m["type"], &typ); err != nil || typ != "url_verification" {
		return "", false
	}
	if cfg.VerificationToken != "" {
		var token string
		_ = json.Unmarshal(m["token"], &token)
		if token != cfg.VerificationToken {
			return "", false
		}
	}
	var ch string
	if err := json.Unmarshal(m["challenge"], &ch); err != nil || ch == "" {
		return "", false
	}
	return ch, true
}
