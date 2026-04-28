package fitbod

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config mirrors the YAML in classes.yaml.example. Internal field names live
// on the LEFT of each Fields entry; wire field names on the RIGHT. Wire
// field paths can be nested ("attributes.name") for JSON:API responses.
type Config struct {
	Backends map[string]BackendConfig `yaml:"backends"`
	Auth     AuthConfig               `yaml:"auth"`
	Classes  map[string]ClassConfig   `yaml:"classes"`
}

type BackendConfig struct {
	BaseURL  string `yaml:"base_url"`
	Style    string `yaml:"style"`     // "jsonapi" | "rest"
	PageSize int    `yaml:"page_size"` // optional; default applied at call site
}

type AuthConfig struct {
	RefreshURL string `yaml:"refresh_url"`
	Scheme     string `yaml:"scheme"` // "bearer"
}

// ClassConfig configures one resource. Top-level resources have Backend +
// ListPath; nested resources (set inside workout, breakdown inside set)
// instead have NestedIn + NestedPath and inherit no URL of their own.
type ClassConfig struct {
	Backend           string            `yaml:"backend"`
	ListPath          string            `yaml:"list_path"`
	DetailPath        string            `yaml:"detail_path"`
	CountURL          string            `yaml:"count_url"`
	NestedIn          string            `yaml:"nested_in"`
	NestedPath        string            `yaml:"nested_path"`
	Sync              string            `yaml:"sync"` // "incremental" toggles cursor-based fetch
	IncrementalFilter string            `yaml:"incremental_filter"`
	Fields            map[string]string `yaml:"fields"`
	Nested            map[string]string `yaml:"nested"`
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("classes config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("classes config: parse: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("classes config: %w", err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	required := []string{"workout", "set", "exercise"}
	for _, k := range required {
		if _, ok := c.Classes[k]; !ok {
			return fmt.Errorf("missing required class %q", k)
		}
	}
	for k, cc := range c.Classes {
		if cc.NestedIn != "" {
			if _, ok := c.Classes[cc.NestedIn]; !ok {
				return fmt.Errorf("class %q: nested_in references unknown class %q", k, cc.NestedIn)
			}
			if cc.NestedPath == "" {
				return fmt.Errorf("class %q: nested classes require nested_path", k)
			}
			continue
		}
		if cc.Backend == "" {
			return fmt.Errorf("class %q: backend is required for top-level classes", k)
		}
		if _, ok := c.Backends[cc.Backend]; !ok {
			return fmt.Errorf("class %q: backend %q not declared", k, cc.Backend)
		}
		if cc.ListPath == "" && cc.DetailPath == "" {
			return fmt.Errorf("class %q: list_path or detail_path is required", k)
		}
	}
	return nil
}

// Field returns the wire field path for an internal name, or "" if not
// configured.
func (c ClassConfig) Field(internal string) string {
	if c.Fields == nil {
		return ""
	}
	return c.Fields[internal]
}

// MustField is for fields the caller treats as mandatory.
func (c ClassConfig) MustField(internal string) (string, error) {
	f := c.Field(internal)
	if f == "" {
		return "", errors.New("missing field mapping for " + internal)
	}
	return f, nil
}
