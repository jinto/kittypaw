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

	f, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := f.Users[accountID]; ok {
		return fmt.Errorf("%w: %s", ErrLocalUserExists, accountID)
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	f.Users[accountID] = LocalUser{
		AccountID:    accountID,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return s.save(f)
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
	if f.Version == 0 {
		f.Version = localAuthVersion
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

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	if len(salt) == 0 || len(expected) == 0 {
		return passwordParams{}, nil, nil, errors.New("invalid empty password hash component")
	}
	return params, salt, expected, nil
}

func parseArgonParams(encoded string) (passwordParams, error) {
	var p passwordParams
	for _, part := range strings.Split(encoded, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return passwordParams{}, fmt.Errorf("invalid argon2 parameter %q", part)
		}
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
	return p, nil
}
