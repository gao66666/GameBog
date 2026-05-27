package wecom

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MsgCrypt 企业微信回调加解密（与公众号算法一致）。
type MsgCrypt struct {
	token  string
	aesKey []byte
	corpID string
}

func NewMsgCrypt(token, encodingAESKey, corpID string) (*MsgCrypt, error) {
	token = strings.TrimSpace(token)
	corpID = strings.TrimSpace(corpID)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodingAESKey) + "=")
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid WECOM_AES_KEY")
	}
	if token == "" || corpID == "" {
		return nil, fmt.Errorf("wecom token/corp_id required")
	}
	return &MsgCrypt{token: token, aesKey: key, corpID: corpID}, nil
}

func (c *MsgCrypt) sha1Sign(parts ...string) string {
	sort.Strings(parts)
	h := sha1.New()
	_, _ = h.Write([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// VerifyURL GET 回调 URL 校验，返回明文 echostr。
func (c *MsgCrypt) VerifyURL(msgSignature, timestamp, nonce, echoStr string) (string, error) {
	if c.sha1Sign(c.token, timestamp, nonce, echoStr) != msgSignature {
		return "", errors.New("invalid wecom signature")
	}
	plain, err := c.decrypt(echoStr)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// DecryptPOST 解密 POST 回调 XML 中的 Encrypt 字段。
func (c *MsgCrypt) DecryptPOST(msgSignature, timestamp, nonce, cipherText string) ([]byte, error) {
	if c.sha1Sign(c.token, timestamp, nonce, cipherText) != msgSignature {
		return nil, errors.New("invalid wecom signature")
	}
	return c.decrypt(cipherText)
}

func (c *MsgCrypt) decrypt(cipherText string) ([]byte, error) {
	cipherData, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return nil, err
	}
	if len(cipherData)%aes.BlockSize != 0 {
		return nil, errors.New("invalid cipher block size")
	}
	iv := c.aesKey[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(cipherData))
	mode.CryptBlocks(plain, cipherData)
	plain, err = pkcs7Unpad(plain)
	if err != nil {
		return nil, err
	}
	if len(plain) < 20 {
		return nil, errors.New("plain too short")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	end := 20 + int(msgLen)
	if end > len(plain) {
		return nil, errors.New("invalid msg length")
	}
	msg := plain[20:end]
	suffix := string(plain[end:])
	if suffix != c.corpID {
		return nil, fmt.Errorf("wecom receiver id mismatch")
	}
	return msg, nil
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	pad := int(data[len(data)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for i := len(data) - pad; i < len(data); i++ {
		if int(data[i]) != pad {
			return nil, errors.New("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-pad], nil
}
