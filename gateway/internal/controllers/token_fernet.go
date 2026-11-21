package controllers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	fernetVersion      byte = 0x80
	tokenPrefixMaster       = "GOTD:M"
	tokenPrefixSession      = "GOTD:S"
	tokenPrefixBackup       = "GOTD:B"
	fernetPlaintextLen      = 15
)

var reLegacyMasterToken = regexp.MustCompile(`^GOTD:[A-Za-z]{32}$`)

func resolveTokenFernetKey() [32]byte {
	if raw := strings.TrimSpace(os.Getenv("TOKEN_FERNET_KEY")); raw != "" {
		for _, dec := range []*base64.Encoding{
			base64.URLEncoding,
			base64.RawURLEncoding,
			base64.StdEncoding,
			base64.RawStdEncoding,
		} {
			if b, err := dec.DecodeString(raw); err == nil && len(b) == 32 {
				var out [32]byte
				copy(out[:], b)
				return out
			}
		}
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			var out [32]byte
			copy(out[:], b)
			return out
		}
		return sha256.Sum256([]byte("gotdbot:fernet:key:v1:" + raw))
	}
	if ses := strings.TrimSpace(os.Getenv("SESSION_ENCRYPTION_KEY")); ses != "" {
		return sha256.Sum256([]byte("gotdbot:fernet:from-session-key:v1:" + ses))
	}
	if bt := strings.TrimSpace(os.Getenv("BOT_TOKEN")); bt != "" {
		return sha256.Sum256([]byte("gotdbot:fernet:from-bot-token:v1:" + bt))
	}
	return sha256.Sum256([]byte("gotdbot:fernet:default-fallback:v1"))
}

func encryptFernetDoubleB64(plain []byte) (string, error) {
	key := resolveTokenFernetKey()
	signingKey := key[:16]
	encKey := key[16:32]

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", fmt.Errorf("new aes cipher: %w", err)
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate fernet iv: %w", err)
	}

	padLen := aes.BlockSize - (len(plain) % aes.BlockSize)
	padded := make([]byte, len(plain)+padLen)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	body := make([]byte, 0, 1+8+aes.BlockSize+len(ciphertext)+sha256.Size)
	body = append(body, fernetVersion)
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(time.Now().UTC().Unix()))
	body = append(body, tsBuf[:]...)
	body = append(body, iv...)
	body = append(body, ciphertext...)

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(body)
	raw := mac.Sum(body)

	single := base64.URLEncoding.EncodeToString(raw)
	return base64.RawURLEncoding.EncodeToString([]byte(single)), nil
}

func decodeBase64Any(s string) ([]byte, error) {
	clean := strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.StdEncoding,
		base64.RawStdEncoding,
	} {
		if b, err := enc.DecodeString(clean); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 encoding")
}

func decryptFernetDoubleB64(encoded string) ([]byte, int64, error) {
	innerBytes, err := decodeBase64Any(encoded)
	if err != nil {
		return nil, 0, fmt.Errorf("decode outer fernet base64: %w", err)
	}
	raw, err := decodeBase64Any(string(innerBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("decode inner fernet base64: %w", err)
	}

	const minFernetLen = 1 + 8 + aes.BlockSize + aes.BlockSize + sha256.Size
	if len(raw) < minFernetLen || raw[0] != fernetVersion {
		return nil, 0, fmt.Errorf("invalid fernet token header or length")
	}

	bodyEnd := len(raw) - sha256.Size
	body := raw[:bodyEnd]
	givenMAC := raw[bodyEnd:]

	key := resolveTokenFernetKey()
	signingKey := key[:16]
	encKey := key[16:32]

	mac := hmac.New(sha256.New, signingKey)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), givenMAC) {
		return nil, 0, fmt.Errorf("invalid fernet token signature")
	}

	ts := int64(binary.BigEndian.Uint64(raw[1:9]))
	iv := raw[9:25]
	ct := raw[25:bodyEnd]
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, 0, fmt.Errorf("invalid fernet ciphertext length")
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, 0, fmt.Errorf("new aes cipher: %w", err)
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)

	padLen := int(plain[len(plain)-1])
	if padLen < 1 || padLen > aes.BlockSize || padLen > len(plain) {
		return nil, 0, fmt.Errorf("invalid pkcs7 padding")
	}
	for i := len(plain) - padLen; i < len(plain); i++ {
		if int(plain[i]) != padLen {
			return nil, 0, fmt.Errorf("corrupt pkcs7 padding")
		}
	}
	return plain[:len(plain)-padLen], ts, nil
}

func GenerateMasterToken() string {
	plain := make([]byte, fernetPlaintextLen)
	plain[0] = 'M'
	plain[1] = 1
	_, _ = rand.Read(plain[2:])
	tok, err := encryptFernetDoubleB64(plain)
	if err != nil {
		return ""
	}
	return tokenPrefixMaster + tok
}

func GenerateSessionToken(masterToken string, telegramUserID int64) string {
	plain := make([]byte, fernetPlaintextLen)
	plain[0] = 'S'
	plain[1] = 1
	binary.BigEndian.PutUint64(plain[2:10], uint64(telegramUserID))
	_, _ = rand.Read(plain[10:])
	tok, err := encryptFernetDoubleB64(plain)
	if err != nil {
		return ComputeLegacyTokenSHA(masterToken, telegramUserID)
	}
	return tokenPrefixSession + tok
}

func ComputeTokenSHA(masterToken string, telegramUserID int64) string {
	return GenerateSessionToken(masterToken, telegramUserID)
}

func ComputeRotatedTokenSHA(masterToken string, telegramUserID, nonce int64) string {
	return GenerateSessionToken(masterToken, telegramUserID)
}

func ComputeLegacyTokenSHA(masterToken string, telegramUserID int64) string {
	payload := fmt.Sprintf("%s:%d", strings.TrimSpace(masterToken), telegramUserID)
	hash := sha256.Sum256([]byte(payload))
	return "GOTD_SHA:" + hex.EncodeToString(hash[:])
}

func DecodeFernetMasterToken(token string) (int64, error) {
	trimmed := strings.TrimSpace(token)
	if !strings.HasPrefix(trimmed, tokenPrefixMaster) {
		return 0, fmt.Errorf("missing GOTD:M prefix")
	}
	plain, ts, err := decryptFernetDoubleB64(strings.TrimPrefix(trimmed, tokenPrefixMaster))
	if err != nil {
		return 0, err
	}
	if len(plain) != fernetPlaintextLen || plain[0] != 'M' || plain[1] != 1 {
		return 0, fmt.Errorf("invalid master token payload")
	}
	return ts, nil
}

func DecodeFernetSessionToken(token string) (int64, int64, error) {
	trimmed := strings.TrimSpace(token)
	if !strings.HasPrefix(trimmed, tokenPrefixSession) {
		return 0, 0, fmt.Errorf("missing GOTD:S prefix")
	}
	plain, ts, err := decryptFernetDoubleB64(strings.TrimPrefix(trimmed, tokenPrefixSession))
	if err != nil {
		return 0, 0, err
	}
	if len(plain) != fernetPlaintextLen || plain[0] != 'S' || plain[1] != 1 {
		return 0, 0, fmt.Errorf("invalid session token payload")
	}
	uid := int64(binary.BigEndian.Uint64(plain[2:10]))
	return uid, ts, nil
}

func IsSessionTokenPrefix(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return strings.HasPrefix(trimmed, tokenPrefixSession) || strings.HasPrefix(trimmed, "GOTD_SHA:")
}

func IsBackupCodePrefix(raw string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	return strings.HasPrefix(trimmed, tokenPrefixBackup) || strings.HasPrefix(trimmed, "GOTD_BKP:")
}

func IsMasterTokenPrefix(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, tokenPrefixMaster) {
		return true
	}
	if strings.HasPrefix(trimmed, tokenPrefixSession) || strings.HasPrefix(trimmed, tokenPrefixBackup) {
		return false
	}
	return strings.HasPrefix(trimmed, "GOTD:")
}

func IsMasterTokenFormat(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, tokenPrefixMaster) {
		_, err := DecodeFernetMasterToken(trimmed)
		return err == nil
	}
	return reLegacyMasterToken.MatchString(trimmed)
}
