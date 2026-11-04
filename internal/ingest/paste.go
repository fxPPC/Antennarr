package ingest

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"
)

// Implements the documented paste.sh v1/v2/v3 wire format with a supplied link
// key. This is format decoding, not password guessing or storage encryption.
func decryptPaste(id, fragment, contentType, etag string, body []byte) ([]byte, error) {
	bad := errors.New("paste decode failed")
	line, rest, ok := bytes.Cut(body, []byte("\n"))
	if !ok || len(line) > 256 {
		return nil, bad
	}
	serverKey := strings.TrimSpace(string(line))
	key := []byte(id + serverKey + fragment + "https://paste.sh")
	full := strings.Join(strings.Fields(string(rest)), "")
	encoded, err := base64.StdEncoding.DecodeString(full)
	if err != nil {
		return nil, bad
	}
	if etag != "" {
		mac := hmac.New(sha512.New, []byte("auth key"))
		mac.Write(key)
		auth := mac.Sum(nil)
		mac = hmac.New(sha512.New, auth)
		mac.Write(encoded)
		expected := base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(strings.Trim(etag, "\"")), []byte(expected)) {
			return nil, bad
		}
	}
	ct := strings.ToLower(contentType)
	if !strings.HasPrefix(ct, "text/plain") && !strings.HasPrefix(ct, "text/vnd.paste.sh-v2") && !strings.HasPrefix(ct, "text/vnd.paste.sh-v3") {
		return nil, bad
	}
	if fragment == "" && !strings.HasPrefix(id, "p") {
		return nil, bad
	}
	payload, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(rest)), ""))
	if err != nil || len(payload) < 32 || string(payload[:8]) != "Salted__" {
		return nil, bad
	}
	salt := payload[8:16]
	data := payload[16:]
	if len(data)%aes.BlockSize != 0 {
		return nil, bad
	}
	var derived []byte
	if strings.HasPrefix(ct, "text/plain") { // Legacy OpenSSL EVP_BytesToKey (SHA-512).
		h := sha512.New()
		h.Write(key)
		h.Write(salt)
		derived = h.Sum(nil)[:48]
	} else {
		derived, err = pbkdf2.Key(sha512.New, string(key), salt, 1, 48)
		if err != nil {
			return nil, bad
		}
	}
	block, err := aes.NewCipher(derived[:32])
	if err != nil {
		return nil, bad
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, derived[32:48]).CryptBlocks(plain, data)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > 16 || pad > len(plain) {
		return nil, bad
	}
	for _, v := range plain[len(plain)-pad:] {
		if int(v) != pad {
			return nil, bad
		}
	}
	plain = plain[:len(plain)-pad]
	if !utf8.Valid(plain) {
		return nil, bad
	}
	if strings.HasPrefix(ct, "text/vnd.paste.sh-v3") {
		plain = bytes.ReplaceAll(plain, []byte("\r\n"), []byte("\n"))
		_, body, ok := bytes.Cut(plain, []byte("\n\n"))
		if !ok {
			return nil, bad
		}
		plain = body
	}
	return plain, nil
}
