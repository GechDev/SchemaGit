// Package config loads schemagit.yaml. Connection details are never stored in
// the repository: every environment names the environment variable that holds
// its PostgreSQL URL, and the URL is read from the process environment only.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the fixed repository configuration file name.
const FileName = "schemagit.yaml"

// Version is the only configuration schema version supported.
const Version = 1

// Config is the parsed schemagit.yaml document.
type Config struct {
	Version       int                    `yaml:"version"`
	DefaultBranch string                 `yaml:"default_branch"`
	Environments  map[string]Environment `yaml:"environments"`
	Safety        Safety                 `yaml:"safety"`
	LinkedGit     LinkedGit              `yaml:"linked_git"`

	// Path records where the document was loaded from.
	Path string `yaml:"-"`
	// Explicit reports whether the file existed on disk.
	Explicit bool `yaml:"-"`
}

// Environment describes one target database.
type Environment struct {
	URLEnv   string `yaml:"url_env"`
	ReadOnly bool   `yaml:"read_only"`
}

// Safety holds the destructive change policy switches.
type Safety struct {
	BlockDestructive *bool `yaml:"block_destructive"`
	// AllowTypesWithoutIndex lists FK rules that may be waived explicitly.
	AllowTypesWithoutIndex []string `yaml:"allow_types_without_index"`
}

// LinkedGit records the optional Git interoperability switch.
type LinkedGit struct {
	Enabled bool `yaml:"enabled"`
}

// Default returns the configuration used when no schemagit.yaml exists.
func Default() *Config {
	blocked := true
	return &Config{
		Version:       Version,
		DefaultBranch: "main",
		Environments:  map[string]Environment{},
		Safety:        Safety{BlockDestructive: &blocked},
	}
}

// BlockDestructive reports whether destructive migrations must be blocked. It
// defaults to true so an incomplete configuration still fails closed.
func (c *Config) BlockDestructive() bool {
	if c == nil || c.Safety.BlockDestructive == nil {
		return true
	}
	return *c.Safety.BlockDestructive
}

// Default returns the built-in configuration for environments without a file.
func (e Environment) Default() bool {
	return e.URLEnv != ""
}

// Find walks up from dir looking for a schemagit.yaml file. When none exists the
// built-in defaults are returned, so every command works in a bare directory.
func Find(dir string) (*Config, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	for {
		candidate := filepath.Join(current, FileName)
		info, err := os.Stat(candidate)
		switch {
		case err == nil && !info.IsDir():
			return Load(candidate)
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("inspect %s: %w", candidate, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return Default(), nil
		}
		current = parent
	}
}

// Load reads and validates a schemagit.yaml document. Unknown keys are rejected
// so a typo cannot silently disable a safety switch.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	config := &Config{}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	config.Path = path
	config.Explicit = true
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

// Validate checks the invariants SchemaGit relies on.
func (c *Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported config version %d, expected %d", c.Version, Version)
	}
	if strings.TrimSpace(c.DefaultBranch) == "" {
		return fmt.Errorf("default_branch must not be empty")
	}
	if strings.ContainsAny(c.DefaultBranch, " \t~^:?*[\\") {
		return fmt.Errorf("invalid default_branch %q", c.DefaultBranch)
	}
	if c.Environments == nil {
		c.Environments = map[string]Environment{}
	}
	for name, environment := range c.Environments {
		if err := validateEnvironmentName(name); err != nil {
			return err
		}
		if strings.TrimSpace(environment.URLEnv) == "" {
			return fmt.Errorf("environment %q must set url_env; inline URLs are not allowed", name)
		}
		if !validEnvVarName(environment.URLEnv) {
			return fmt.Errorf("environment %q has an invalid url_env %q", name, environment.URLEnv)
		}
	}
	if c.LinkedGit.Enabled {
		return fmt.Errorf("linked_git is not implemented in this release")
	}
	return nil
}

func validateEnvironmentName(name string) error {
	if name == "" {
		return fmt.Errorf("environment name must not be empty")
	}
	if strings.ContainsAny(name, " \t/\\\"'") {
		return fmt.Errorf("invalid environment name %q", name)
	}
	return nil
}

func validEnvVarName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		switch {
		case character >= 'A' && character <= 'Z':
		case character == '_':
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9' && index > 0:
		default:
			return false
		}
	}
	return true
}

// EnvironmentNames lists configured environments in ascending order.
func (c *Config) EnvironmentNames() []string {
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Environment looks up a configured environment.
func (c *Config) Environment(name string) (Environment, error) {
	if name == "" {
		return Environment{}, fmt.Errorf("environment name must not be empty")
	}
	environment, ok := c.Environments[name]
	if !ok {
		known := strings.Join(c.EnvironmentNames(), ", ")
		if known == "" {
			return Environment{}, fmt.Errorf("unknown environment %q: no environments are configured", name)
		}
		return Environment{}, fmt.Errorf("unknown environment %q: configured environments are %s", name, known)
	}
	return environment, nil
}

// URL resolves the environment's PostgreSQL URL from the process environment.
func (e Environment) URL() (string, error) {
	if !e.Default() {
		return "", fmt.Errorf("environment does not define url_env")
	}
	value, ok := os.LookupEnv(e.URLEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("environment variable %s is not set", e.URLEnv)
	}
	return value, nil
}

// URL resolves a named environment to its PostgreSQL URL.
func (c *Config) URL(name string) (Environment, string, error) {
	environment, err := c.Environment(name)
	if err != nil {
		return Environment{}, "", err
	}
	url, err := environment.URL()
	if err != nil {
		return Environment{}, "", fmt.Errorf("environment %q: %w", name, err)
	}
	return environment, url, nil
}
