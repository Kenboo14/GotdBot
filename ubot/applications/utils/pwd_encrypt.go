package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/nacl/box"
)

func EncryptPWD(version byte, publicKeyHex string, plaintext string) (string, error) {
	if len(publicKeyHex) != 64 {
		return "", errors.New("public key length error")
	}

	// Timestamp
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	additionalData := []byte(timestamp)
	plaintextBytes := []byte(plaintext)

	// Parse public key
	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return "", err
	}
	var publicKey [32]byte
	copy(publicKey[:], pubKeyBytes)

	// Generate AES-256 key
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return "", err
	}

	// Seal AES key using NaCl (crypto_box_seal)
	sealedKey, err := box.SealAnonymous(nil, aesKey, &publicKey, rand.Reader)
	if err != nil {
		return "", err
	}

	// AES-GCM encryption
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	iv := make([]byte, 12) // all-zero IV (matches JS)
	ciphertextWithTag := gcm.Seal(nil, iv, plaintextBytes, additionalData)

	// Split ciphertext and auth tag
	tag := ciphertextWithTag[len(ciphertextWithTag)-16:]
	ciphertext := ciphertextWithTag[:len(ciphertextWithTag)-16]

	// Build final buffer
	bufferLen := 1 + 1 + 2 + len(sealedKey) + 16 + len(ciphertext)
	buffer := make([]byte, bufferLen)

	offset := 0
	buffer[offset] = 1
	offset++
	buffer[offset] = version
	offset++

	binary.LittleEndian.PutUint16(buffer[offset:], uint16(len(sealedKey)))
	offset += 2

	copy(buffer[offset:], sealedKey)
	offset += len(sealedKey)

	copy(buffer[offset:], tag)
	offset += 16

	copy(buffer[offset:], ciphertext)
	offset += len(ciphertext)

	finalBuffer := buffer[:offset]

	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString(finalBuffer)

	return fmt.Sprintf("#PWD_BROWSER:5:%s:%s", timestamp, encoded), nil
}

func DecodeUTF8(str string) ([]byte, error) {
	if str == "" {
		return nil, errors.New("expected non-empty string")
	}
	return []byte(str), nil
}

// ParsePublicKey converts a hex string into a byte slice
func ParsePublicKey(hexStr string) ([]byte, error) {
	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

// EncodeBase64 encodes a byte slice into a Base64 string
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
