package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrAccountExists = errors.New("account already exists")

type AccountOpts struct {
	TelegramToken string
	AdminChatID   string
	IsFamily      bool
	LLMProvider   string
	LLMAPIKey     string
	LLMModel      string
	LocalPassword string
}

// InitAccount stages the tree under `.<id>.staging/` and renames into place,
// so a crash mid-flight leaves nothing behind for the caller to clean up.
func InitAccount(accountsDir, id string, opts AccountOpts) (*Account, error) {
	if err := ValidateAccountID(id); err != nil {
		return nil, err
	}

	if opts.IsFamily && opts.TelegramToken != "" {
		return nil, fmt.Errorf("family account %q must not declare channels (telegram token rejected)", id)
	}

	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create accounts dir: %w", err)
	}

	finalDir := filepath.Join(accountsDir, id)
	if _, err := os.Stat(finalDir); err == nil {
		return nil, fmt.Errorf("%w: %q at %s", ErrAccountExists, id, finalDir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat account dir: %w", err)
	}

	// Must run before DiscoverAccounts so the dotted staging dir doesn't trigger a WARN.
	stagingDir := filepath.Join(accountsDir, "."+id+".staging")
	if err := os.RemoveAll(stagingDir); err != nil {
		return nil, fmt.Errorf("clean stale staging: %w", err)
	}

	if opts.TelegramToken != "" {
		existing, err := DiscoverAccounts(accountsDir)
		if err != nil {
			return nil, fmt.Errorf("discover existing accounts: %w", err)
		}
		for _, peer := range existing {
			if peer.Config != nil {
				for _, ch := range peer.Config.Channels {
					if ch.ChannelType == ChannelTelegram && ch.Token == opts.TelegramToken {
						return nil, fmt.Errorf("telegram bot_token already used by account %q", peer.ID)
					}
				}
			}
			secrets, err := LoadSecretsFrom(filepath.Join(peer.BaseDir, "secrets.json"))
			if err != nil {
				return nil, fmt.Errorf("load account secrets for %q: %w", peer.ID, err)
			}
			if token, ok := secrets.Get("channel/telegram", "bot_token"); ok && token == opts.TelegramToken {
				return nil, fmt.Errorf("telegram bot_token already used by account %q", peer.ID)
			}
		}
	}

	staging := &Account{ID: id, BaseDir: stagingDir}
	if err := staging.EnsureDirs(); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	cfg, err := buildAccountConfig(opts)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	if err := WriteConfigAtomic(cfg, filepath.Join(stagingDir, "config.toml")); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("write config: %w", err)
	}

	var authStore *LocalAuthStore
	if opts.LocalPassword != "" {
		authStore = NewLocalAuthStore(accountsDir)
		has, err := authStore.HasUser(id)
		if err != nil {
			_ = os.RemoveAll(stagingDir)
			return nil, err
		}
		if has {
			_ = os.RemoveAll(stagingDir)
			return nil, fmt.Errorf("%w: %s", ErrLocalUserExists, id)
		}
		hash, err := hashPassword(opts.LocalPassword)
		if err != nil {
			_ = os.RemoveAll(stagingDir)
			return nil, err
		}
		now := time.Now().UTC()
		if err := writeLocalAccountAuthFile(filepath.Join(stagingDir, "account.toml"), LocalUser{
			AccountID:    id,
			PasswordHash: hash,
			CreatedAt:    now,
			UpdatedAt:    now,
		}); err != nil {
			_ = os.RemoveAll(stagingDir)
			return nil, err
		}
	}
	secrets, err := LoadSecretsFrom(filepath.Join(stagingDir, "secrets.json"))
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}
	if err := SaveWizardSecretsTo(secrets, WizardResult{
		LLMProvider:      opts.LLMProvider,
		LLMAPIKey:        opts.LLMAPIKey,
		LLMModel:         opts.LLMModel,
		TelegramBotToken: opts.TelegramToken,
		TelegramChatID:   opts.AdminChatID,
	}, cfg); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	if err := os.Rename(stagingDir, finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		if authStore != nil {
			_ = authStore.DeleteUser(id)
		}
		return nil, fmt.Errorf("commit staging → %s: %w", finalDir, err)
	}

	return &Account{ID: id, BaseDir: finalDir, Config: cfg}, nil
}

// buildAccountConfig shares defaults with the TTY wizard via MergeWizardSettings.
func buildAccountConfig(opts AccountOpts) (*Config, error) {
	w := WizardResult{
		LLMProvider:      opts.LLMProvider,
		LLMAPIKey:        opts.LLMAPIKey,
		LLMModel:         opts.LLMModel,
		TelegramBotToken: opts.TelegramToken,
		TelegramChatID:   opts.AdminChatID,
	}
	base := DefaultConfig()
	cfg := MergeWizardSettings(&base, w)
	cfg.IsShared = opts.IsFamily
	if _, err := EnsureServerAPIKey(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
