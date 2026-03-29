package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	phcPartsLen      = 6
	phcAlgoPartIndex = 1
	phcVersionIndex  = 2
	phcParamsIndex   = 3
	phcSaltIndex     = 4
	phcHashIndex     = 5

	defaultMemoryKiB    = 64 * 1024
	defaultIterations   = 3
	defaultParallelism  = 2
	defaultSaltLenBytes = 16
	defaultKeyLenBytes  = 32

	maxMemoryKiB     = 1024 * 1024
	maxIterations    = 10
	maxParallelism   = 8
	maxHashLenBytes  = 128
	minimumParamsVal = 1
)

type Argon2 struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

func NewArgon2() *Argon2 {
	return &Argon2{
		memory:      defaultMemoryKiB,
		iterations:  defaultIterations,
		parallelism: defaultParallelism,
		saltLen:     defaultSaltLenBytes,
		keyLen:      defaultKeyLenBytes,
	}
}

func (a *Argon2) Hash(password string) (string, error) {
	salt := make([]byte, a.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		a.iterations,
		a.memory,
		a.parallelism,
		a.keyLen,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		a.memory,
		a.iterations,
		a.parallelism,
		b64Salt,
		b64Hash,
	), nil
}

type parsedPHC struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	hash        []byte
}

func (a *Argon2) Compare(encodedHash, plainPassword string) (bool, error) {
	parsed, err := parsePHCEncodedHash(encodedHash)
	if err != nil {
		return false, err
	}

	hashLen := len(parsed.hash)
	if hashLen > maxHashLenBytes {
		return false, fmt.Errorf("hash length exceeds maximum: %d > %d", hashLen, maxHashLenBytes)
	}

	calculatedHash := argon2.IDKey(
		[]byte(plainPassword),
		parsed.salt,
		parsed.iterations,
		parsed.memory,
		parsed.parallelism,
		uint32(hashLen),
	)

	return subtle.ConstantTimeCompare(parsed.hash, calculatedHash) == 1, nil
}

func parsePHCEncodedHash(encodedHash string) (*parsedPHC, error) {
	parts := strings.Split(encodedHash, "$")
	if err := validatePHCParts(parts); err != nil {
		return nil, err
	}

	memory, iterations, parallelism, err := parsePHCParams(parts[phcParamsIndex])
	if err != nil {
		return nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[phcSaltIndex])
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[phcHashIndex])
	if err != nil {
		return nil, fmt.Errorf("decode hash: %w", err)
	}

	return &parsedPHC{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		salt:        salt,
		hash:        decodedHash,
	}, nil
}

func validatePHCParts(parts []string) error {
	if len(parts) != phcPartsLen {
		return errors.New("invalid encoded hash format")
	}
	if parts[phcAlgoPartIndex] != "argon2id" {
		return errors.New("invalid algorithm")
	}

	var version int
	if _, err := fmt.Sscanf(parts[phcVersionIndex], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	if version != argon2.Version {
		return errors.New("incompatible argon2 version")
	}

	return nil
}

func parsePHCParams(raw string) (uint32, uint32, uint8, error) {
	var memory uint32
	var iterations uint32
	var parallelism uint8

	if _, err := fmt.Sscanf(raw, "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, fmt.Errorf("parse params: %w", err)
	}
	if memory < minimumParamsVal || memory > maxMemoryKiB {
		return 0, 0, 0, errors.New("invalid memory cost")
	}
	if iterations < minimumParamsVal || iterations > maxIterations {
		return 0, 0, 0, errors.New("invalid iteration cost")
	}
	if parallelism < minimumParamsVal || parallelism > maxParallelism {
		return 0, 0, 0, errors.New("invalid parallelism")
	}

	return memory, iterations, parallelism, nil
}
