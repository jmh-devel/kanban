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

	DefaultRunner = "codex"
	ManualRunner  = "manual"
	DefaultMode   = "implement"
)

type Config struct {
	Repos       map[string]RepoConfig   `json:"repos,omitempty"`
	Runners     map[string]RunnerConfig `json:"runners,omitempty"`
	Agent       AgentConfig             `json:"agent,omitempty"`
	ReviewAgent ReviewAgentConfig       `json:"review_agent,omitempty"`
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

type ReviewAgentConfig struct {
	Runner       string `json:"runner,omitempty"`
	Mode         string `json:"mode,omitempty"`
	AutoMerge    *bool  `json:"auto_merge,omitempty"`
	DeleteBranch *bool  `json:"delete_branch,omitempty"`
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
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&config)
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func SaveConfig(config Config) error {
	applyDefaults(&config)
	if err := validateConfig(config); err != nil {
		return err
	}
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
	seen := map[string]bool{}
	for name := range c.Runners {
		names = append(names, name)
		seen[name] = true
	}
	sort.Strings(names)
	if !seen[DefaultRunner] {
		names = append(names, DefaultRunner)
	}
	if !seen[ManualRunner] {
		names = append(names, ManualRunner)
	}
	return names
}

func (c Config) Runner(name string) RunnerConfig {
	if name == ManualRunner {
		return RunnerConfig{Kind: ManualRunner}
	}
	runner, ok := c.Runners[name]
	if !ok {
		if name == DefaultRunner {
			return RunnerConfig{Kind: "local_cli", Command: DefaultRunner}
		}
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

func (c Config) ReviewRunner() string {
	if runner := strings.TrimSpace(c.ReviewAgent.Runner); runner != "" {
		return runner
	}
	return DefaultRunner
}

func (c Config) ReviewMode() string {
	if mode := strings.TrimSpace(c.ReviewAgent.Mode); mode != "" {
		return mode
	}
	return "auto"
}

func (c Config) ReviewAutoMerge() bool {
	if c.ReviewAgent.AutoMerge == nil {
		return true
	}
	return *c.ReviewAgent.AutoMerge
}

func (c Config) ReviewDeleteBranch() bool {
	if c.ReviewAgent.DeleteBranch == nil {
		return true
	}
	return *c.ReviewAgent.DeleteBranch
}

func DefaultConfig() Config {
	autoMove := true
	autoComplete := false
	autoMerge := true
	deleteBranch := true
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
		ReviewAgent: ReviewAgentConfig{
			Runner:       DefaultRunner,
			Mode:         "auto",
			AutoMerge:    &autoMerge,
			DeleteBranch: &deleteBranch,
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
	if strings.TrimSpace(config.ReviewAgent.Runner) == "" {
		config.ReviewAgent.Runner = DefaultRunner
	}
	if strings.TrimSpace(config.ReviewAgent.Mode) == "" {
		config.ReviewAgent.Mode = "auto"
	}
	if config.ReviewAgent.AutoMerge == nil {
		v := true
		config.ReviewAgent.AutoMerge = &v
	}
	if config.ReviewAgent.DeleteBranch == nil {
		v := true
		config.ReviewAgent.DeleteBranch = &v
	}
}

func validateConfig(config Config) error {
	switch config.ReviewMode() {
	case "auto", "manual":
		return nil
	default:
		return fmt.Errorf("review_agent.mode must be auto or manual, got %q", config.ReviewMode())
	}
}
