package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ConfigFileName     = "config.json"
	DispatchesFileName = "dispatches.json"

	DefaultRunner = "manual"
	DefaultMode   = "implement"
)

type Config struct {
	Repos   map[string]RepoConfig   `json:"repos,omitempty"`
	Runners map[string]RunnerConfig `json:"runners,omitempty"`
	Agent   AgentConfig             `json:"agent,omitempty"`
}

type RepoConfig struct {
	RepoKey         string `json:"repo_key,omitempty"`
	ReposFile       string `json:"repos_file,omitempty"`
	PreferredRunner string `json:"preferred_runner,omitempty"`
	PreferredMode   string `json:"preferred_mode,omitempty"`
}

type AgentConfig struct {
	AutoMoveOnDispatch *bool `json:"auto_move_on_dispatch,omitempty"`
	AutoMoveOnComplete *bool `json:"auto_move_on_complete,omitempty"`
}

type RunnerConfig struct {
	Kind         string             `json:"kind,omitempty"`
	Command      string             `json:"command,omitempty"`
	Capabilities RunnerCapabilities `json:"capabilities,omitempty"`
}

type RunnerCapabilities struct {
	SupportsPlanning      bool `json:"supports_planning,omitempty"`
	SupportsImplement     bool `json:"supports_implement,omitempty"`
	SupportsReview        bool `json:"supports_review,omitempty"`
	SupportsStreamingLogs bool `json:"supports_streaming_logs,omitempty"`
	SupportsCancel        bool `json:"supports_cancel,omitempty"`
	RequiresRepoKey       bool `json:"requires_repo_key,omitempty"`
}

func ConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("KANBAN_CONFIG_DIR")); dir != "" {
		return dir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kanban"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
}

func DispatchesPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DispatchesFileName), nil
}

func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultConfig(), nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&config)
	return config, nil
}

func SaveConfig(config Config) error {
	applyDefaults(&config)
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) RunnerNames() []string {
	names := make([]string, 0, len(c.Runners)+1)
	for name := range c.Runners {
		names = append(names, name)
	}
	sort.Strings(names)
	names = append(names, DefaultRunner)
	return names
}

func (c Config) Runner(name string) RunnerConfig {
	if name == DefaultRunner || len(c.Runners) == 0 {
		return RunnerConfig{Kind: DefaultRunner}
	}
	runner, ok := c.Runners[name]
	if !ok {
		return RunnerConfig{}
	}
	return runner
}

func (c Config) AutoMoveOnDispatch() bool {
	if c.Agent.AutoMoveOnDispatch == nil {
		return true
	}
	return *c.Agent.AutoMoveOnDispatch
}

func defaultConfig() Config {
	autoMove := true
	autoComplete := false
	config := Config{
		Repos: map[string]RepoConfig{},
		Runners: map[string]RunnerConfig{
			"codex": {
				Kind:    "local_cli",
				Command: "codex",
				Capabilities: RunnerCapabilities{
					SupportsPlanning:  true,
					SupportsImplement: true,
				},
			},
			"claude": {
				Kind:    "local_cli",
				Command: "claude",
				Capabilities: RunnerCapabilities{
					SupportsPlanning:  true,
					SupportsImplement: true,
					SupportsReview:    true,
				},
			},
			"tsctl": {
				Kind: "tsctl_dispatch",
				Capabilities: RunnerCapabilities{
					SupportsPlanning:  true,
					SupportsImplement: true,
					SupportsReview:    true,
					SupportsCancel:    true,
					RequiresRepoKey:   true,
				},
			},
		},
		Agent: AgentConfig{
			AutoMoveOnDispatch: &autoMove,
			AutoMoveOnComplete: &autoComplete,
		},
	}
	return config
}

func applyDefaults(config *Config) {
	if config.Repos == nil {
		config.Repos = map[string]RepoConfig{}
	}
	if config.Runners == nil {
		config.Runners = map[string]RunnerConfig{}
	}
	if config.Agent.AutoMoveOnDispatch == nil {
		v := true
		config.Agent.AutoMoveOnDispatch = &v
	}
	if config.Agent.AutoMoveOnComplete == nil {
		v := false
		config.Agent.AutoMoveOnComplete = &v
	}
}
