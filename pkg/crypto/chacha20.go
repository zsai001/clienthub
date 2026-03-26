package crypto

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	KeySize   = chacha20poly1305.KeySize   // 32 bytes
	NonceSize = chacha20poly1305.NonceSizeX // 24 bytes for XChaCha20
	SaltSize  = 16
)

type Cipher struct {
	key  []byte
	aead cipher.AEAD // cached, reused across calls
}

func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, KeySize)
}

func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("invalid key size: got %d, want %d", len(key), KeySize)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create AEAD: %w", err)
	}
	return &Cipher{key: key, aead: aead}, nil
}

func NewCipherFromPassword(password string, salt []byte) *Cipher {
	key := DeriveKey(password, salt)
	aead, _ := chacha20poly1305.NewX(key)
	return &Cipher{key: key, aead: aead}
}

func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	// nonce || ciphertext+tag
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	if len(data) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce := data[:NonceSize]
	ciphertext := data[NonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func (c *Cipher) Key() []byte {
	return c.key
}

func ComputeAuthToken(clientName string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(clientName))
	return mac.Sum(nil)
}

func VerifyAuthToken(clientName string, token, key []byte) bool {
	expected := ComputeAuthToken(clientName, key)
	return hmac.Equal(token, expected)
}
