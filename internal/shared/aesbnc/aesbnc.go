// Package aesbnc 对齐公司封面加密: AES-128-ECB + PKCS7, 密钥与 AesUtil::decryptRaw 相同。
package aesbnc

import (
	"crypto/aes"
	"errors"
	"path/filepath"
	"strings"
)

const DefaultKey = "dafb787c76b5ce8a"
const Key = DefaultKey
const Ext = ".bnc"

var activeKey = DefaultKey

func SetKey(k string) {
	k = strings.TrimSpace(k)
	if len(k) != aes.BlockSize {
		activeKey = DefaultKey
		return
	}
	activeKey = k
}

func keyBytes() []byte { return []byte(activeKey) }

func Encrypt(plain []byte) ([]byte, error) {
	if plain == nil {
		plain = []byte{}
	}
	block, err := aes.NewCipher(keyBytes())
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return out, nil
}

func Decrypt(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度无效")
	}
	block, err := aes.NewCipher(keyBytes())
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	return pkcs7Unpad(out, aes.BlockSize)
}

func pkcs7Pad(b []byte, n int) []byte {
	pad := n - (len(b) % n)
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(b []byte, n int) ([]byte, error) {
	if len(b) == 0 || len(b)%n != 0 {
		return nil, errors.New("填充无效")
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > n || pad > len(b) {
		return nil, errors.New("填充无效")
	}
	for i := len(b) - pad; i < len(b); i++ {
		if b[i] != byte(pad) {
			return nil, errors.New("填充无效")
		}
	}
	return b[:len(b)-pad], nil
}

func IsEncryptedName(name string) bool {
	low := strings.ToLower(name)
	if i := strings.IndexAny(low, "?#"); i >= 0 {
		low = low[:i]
	}
	return strings.HasSuffix(low, ".bnc") || strings.HasSuffix(low, ".ceb")
}

func LooksLikeImage(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	if b[0] == 0xFF && b[1] == 0xD8 {
		return true
	}
	if b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47 {
		return true
	}
	if b[0] == 0x47 && b[1] == 0x49 && b[2] == 0x46 {
		return true
	}
	if string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return true
	}
	return false
}

func ToBncKey(key string) string {
	if key == "" || IsEncryptedName(key) {
		return key
	}
	ext := filepath.Ext(key)
	if ext == "" {
		return key + Ext
	}
	return strings.TrimSuffix(key, ext) + Ext
}

func SniffContentType(b []byte) string {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 {
		return "image/jpeg"
	}
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 0x50 {
		return "image/png"
	}
	if len(b) >= 3 && b[0] == 0x47 && b[1] == 0x49 {
		return "image/gif"
	}
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "application/octet-stream"
}
