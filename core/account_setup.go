package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		tc := make(map[string][]ChannelConfig, len(existing)+1)
		for _, peer := range existing {
			if peer.Config != nil {
				tc[peer.ID] = peer.Config.Channels
			}
		}
		tc[id] = []ChannelConfig{{ChannelType: ChannelTelegram, Token: opts.TelegramToken}}
		if err := ValidateAccountChannels(tc); err != nil {
			return nil, err
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
	authCreated := false
	if opts.LocalPassword != "" {
		authPath, err := LocalAuthPath()
		if err != nil {
			_ = os.RemoveAll(stagingDir)
			return nil, err
		}
		authStore = NewLocalAuthStore(authPath)
		if err := authStore.CreateUser(id, opts.LocalPassword); err != nil {
			_ = os.RemoveAll(stagingDir)
			return nil, err
		}
		authCreated = true
	}

	if err := os.Rename(stagingDir, finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		if authCreated {
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
	cfg.IsFamily = opts.IsFamily
	if _, err := EnsureServerAPIKey(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
