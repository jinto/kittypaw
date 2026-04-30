package core

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	localAuthVersion = 1
	argonMemoryKB    = 64 * 1024
	argonTime        = 3
	argonThreads     = 4
	argonKeyLen      = 32
	argonSaltLen     = 16
	lockTimeout      = 5 * time.Second
)

var ErrLocalUserExists = errors.New("local user already exists")

// LocalAuthStore manages server-wide local Web UI credentials.
type LocalAuthStore struct {
	path string
}

type LocalAuthFile struct {
	Version int                  `json:"version"`
	Users   map[string]LocalUser `json:"users"`
}

type LocalUser struct {
	AccountID    string    `json:"account_id"`
	PasswordHash string    `json:"password_hash"`
	Disabled     bool      `json:"disabled,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type passwordParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func NewLocalAuthStore(path string) *LocalAuthStore {
	return &LocalAuthStore{path: path}
}

func LocalAuthPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

func (s *LocalAuthStore) CreateUser(accountID, password string) error {
	if err := ValidateAccountID(accountID); err != nil {
		return err
	}
	if password == "" {
		return errors.New("password is required")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		f, err := s.load()
		if err != nil {
			return err
		}
		if _, ok := f.Users[accountID]; ok {
			return fmt.Errorf("%w: %s", ErrLocalUserExists, accountID)
		}

		now := time.Now().UTC()
		f.Users[accountID] = LocalUser{
			AccountID:    accountID,
			PasswordHash: hash,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		return s.save(f)
	})
}

func (s *LocalAuthStore) HasUser(accountID string) (bool, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return false, err
	}
	f, err := s.load()
	if err != nil {
		return false, err
	}
	_, ok := f.Users[accountID]
	return ok, nil
}

func (s *LocalAuthStore) DeleteUser(accountID string) error {
	if err := ValidateAccountID(accountID); err != nil {
		return err
	}
	return s.withLock(func() error {
		f, err := s.load()
		if err != nil {
			return err
		}
		if _, ok := f.Users[accountID]; !ok {
			return nil
		}
		delete(f.Users, accountID)
		return s.save(f)
	})
}

func (s *LocalAuthStore) VerifyPassword(accountID, password string) (bool, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return false, err
	}
	f, err := s.load()
	if err != nil {
		return false, err
	}
	u, ok := f.Users[accountID]
	if !ok || u.Disabled {
		return false, nil
	}
	return verifyPassword(u.PasswordHash, password)
}

func (s *LocalAuthStore) SetDisabled(accountID string, disabled bool) error {
	if err := ValidateAccountID(accountID); err != nil {
		return err
	}
	return s.withLock(func() error {
		f, err := s.load()
		if err != nil {
			return err
		}
		u, ok := f.Users[accountID]
		if !ok {
			return fmt.Errorf("local user %q not found", accountID)
		}
		u.Disabled = disabled
		u.UpdatedAt = time.Now().UTC()
		f.Users[accountID] = u
		return s.save(f)
	})
}

func (s *LocalAuthStore) load() (*LocalAuthFile, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &LocalAuthFile{Version: localAuthVersion, Users: map[string]LocalUser{}}, nil
	}
	if err != nil {
		return nil, err
	}

	var f LocalAuthFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse local auth store: %w", err)
	}
	switch f.Version {
	case 0:
		f.Version = localAuthVersion
	case localAuthVersion:
	default:
		return nil, fmt.Errorf("unsupported local auth store version %d", f.Version)
	}
	if f.Users == nil {
		f.Users = map[string]LocalUser{}
	}
	return &f, nil
}

func (s *LocalAuthStore) save(f *LocalAuthFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	if f.Version == 0 {
		f.Version = localAuthVersion
	}
	if f.Users == nil {
		f.Users = map[string]LocalUser{}
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	_ = os.Remove(s.path + ".tmp")
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func (s *LocalAuthStore) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	lockPath := s.path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("local auth store lock timeout: %s", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKB, argonThreads, argonKeyLen)
	return "argon2id$v=1$m=65536,t=3,p=4$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(sum), nil
}

func verifyPassword(encoded, password string) (bool, error) {
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parsePasswordHash(encoded string) (passwordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=1" {
		return passwordParams{}, nil, nil, errors.New("invalid password hash format")
	}

	params, err := parseArgonParams(parts[2])
	if err != nil {
		return passwordParams{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("decode password salt: %w", err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return passwordParams{}, nil, nil, fmt.Errorf("decode password hash: %w", err)
	}
	if len(salt) != argonSaltLen {
		return passwordParams{}, nil, nil, fmt.Errorf("invalid password salt length %d", len(salt))
	}
	if len(expected) != argonKeyLen {
		return passwordParams{}, nil, nil, fmt.Errorf("invalid password hash length %d", len(expected))
	}
	return params, salt, expected, nil
}

func parseArgonParams(encoded string) (passwordParams, error) {
	var p passwordParams
	seen := make(map[string]bool, 3)
	for _, part := range strings.Split(encoded, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return passwordParams{}, fmt.Errorf("invalid argon2 parameter %q", part)
		}
		if seen[k] {
			return passwordParams{}, fmt.Errorf("duplicate argon2 parameter %q", k)
		}
		seen[k] = true
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return passwordParams{}, fmt.Errorf("invalid argon2 parameter %q: %w", part, err)
		}
		switch k {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.time = uint32(n)
		case "p":
			if n > 255 {
				return passwordParams{}, fmt.Errorf("argon2 threads too large: %d", n)
			}
			p.threads = uint8(n)
		default:
			return passwordParams{}, fmt.Errorf("unknown argon2 parameter %q", k)
		}
	}
	if p.memory == 0 || p.time == 0 || p.threads == 0 {
		return passwordParams{}, errors.New("missing argon2 parameters")
	}
	if p.memory != argonMemoryKB || p.time != argonTime || p.threads != argonThreads {
		return passwordParams{}, fmt.Errorf("unsupported argon2 parameters m=%d,t=%d,p=%d", p.memory, p.time, p.threads)
	}
	return p, nil
}
