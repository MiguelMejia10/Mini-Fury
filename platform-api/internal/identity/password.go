package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"golang.org/x/crypto/argon2"
	"strings"
)

const hashPrefix = "argon2id-v1"

// Fixed, versioned parameters: 64 MiB, 3 passes, 2 lanes; bound concurrency in Service.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return hashPrefix + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != hashPrefix {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[2])
	if e != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
func Token() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func CSRF(token string) string { return Digest("mini-fury-csrf-v1:" + token) }
