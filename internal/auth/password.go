package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

var (
	dummyPasswordHashOnce sync.Once
	dummyPasswordHash     string
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 2
	passwordSaltLength  = 16
	passwordKeyLength   = 32
	MinPasswordLength   = 12
	MaxPasswordLength   = 128
)

func ValidatePassword(password string) error {
	length := len([]rune(password))
	if length < MinPasswordLength {
		return fmt.Errorf("Password must be at least %d characters.", MinPasswordLength)
	}
	if length > MaxPasswordLength {
		return fmt.Errorf("Password must be no more than %d characters.", MaxPasswordLength)
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordParallelism, passwordKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordIterations,
		passwordParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func CheckPassword(encodedHash, password string) bool {
	memory, iterations, parallelism, salt, expected, err := parsePasswordHash(encodedHash)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parsePasswordHash(encoded string) (uint32, uint32, uint8, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return 0, 0, 0, nil, nil, errors.New("invalid password hash")
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, nil, nil, err
	}
	if memory == 0 || iterations == 0 || parallelism == 0 || memory > 1024*1024 || iterations > 20 {
		return 0, 0, 0, nil, nil, errors.New("invalid password parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(salt) < 8 || len(hash) < 16 {
		return 0, 0, 0, nil, nil, errors.New("invalid password hash payload")
	}
	return memory, iterations, parallelism, salt, hash, nil
}

func DummyPasswordHash() string {
	dummyPasswordHashOnce.Do(func() {
		salt := base64.RawStdEncoding.EncodeToString([]byte("btcpp-password!!"))
		hash := argon2.IDKey([]byte("invalid-password"), []byte("btcpp-password!!"), passwordIterations, passwordMemory, passwordParallelism, passwordKeyLength)
		dummyPasswordHash = "$argon2id$v=19$m=" + strconv.Itoa(passwordMemory) + ",t=" + strconv.Itoa(passwordIterations) + ",p=" + strconv.Itoa(passwordParallelism) + "$" + salt + "$" + base64.RawStdEncoding.EncodeToString(hash)
	})
	return dummyPasswordHash
}
