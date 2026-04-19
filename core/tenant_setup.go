package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrTenantExists = errors.New("tenant already exists")

type TenantOpts struct {
	TelegramToken string
	AdminChatID   string
	IsFamily      bool
	LLMProvider   string
	LLMAPIKey     string
	LLMModel      string
}

// InitTenant stages the tree under `.<id>.staging/` and renames into place,
// so a crash mid-flight leaves nothing behind for the caller to clean up.
func InitTenant(tenantsDir, id string, opts TenantOpts) (*Tenant, error) {
	if err := ValidateTenantID(id); err != nil {
		return nil, err
	}

	if opts.IsFamily && opts.TelegramToken != "" {
		return nil, fmt.Errorf("family tenant %q must not declare channels (telegram token rejected)", id)
	}

	if err := os.MkdirAll(tenantsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create tenants dir: %w", err)
	}

	finalDir := filepath.Join(tenantsDir, id)
	if _, err := os.Stat(finalDir); err == nil {
		return nil, fmt.Errorf("%w: %q at %s", ErrTenantExists, id, finalDir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat tenant dir: %w", err)
	}

	// Must run before DiscoverTenants so the dotted staging dir doesn't trigger a WARN.
	stagingDir := filepath.Join(tenantsDir, "."+id+".staging")
	if err := os.RemoveAll(stagingDir); err != nil {
		return nil, fmt.Errorf("clean stale staging: %w", err)
	}

	if opts.TelegramToken != "" {
		existing, err := DiscoverTenants(tenantsDir)
		if err != nil {
			return nil, fmt.Errorf("discover existing tenants: %w", err)
		}
		tc := make(map[string][]ChannelConfig, len(existing)+1)
		for _, peer := range existing {
			if peer.Config != nil {
				tc[peer.ID] = peer.Config.Channels
			}
		}
		tc[id] = []ChannelConfig{{ChannelType: ChannelTelegram, Token: opts.TelegramToken}}
		if err := ValidateTenantChannels(tc); err != nil {
			return nil, err
		}
	}

	staging := &Tenant{ID: id, BaseDir: stagingDir}
	if err := staging.EnsureDirs(); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	cfg := buildTenantConfig(opts)
	if err := WriteConfigAtomic(cfg, filepath.Join(stagingDir, "config.toml")); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("write config: %w", err)
	}

	if err := os.Rename(stagingDir, finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("commit staging → %s: %w", finalDir, err)
	}

	return &Tenant{ID: id, BaseDir: finalDir, Config: cfg}, nil
}

// buildTenantConfig shares defaults with the TTY wizard via MergeWizardSettings.
func buildTenantConfig(opts TenantOpts) *Config {
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
	return cfg
}
