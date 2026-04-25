package fitbod

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ClassConfig describes one Parse class we want to poll. The keys in Fields
// are stable internal names the rest of the codebase uses (start_time,
// weight_lbs, ...); the values are whatever Fitbod actually calls them.
type ClassConfig struct {
	Name    string         `yaml:"name"`
	Order   string         `yaml:"order"`
	Limit   int            `yaml:"limit"`
	Skip    int            `yaml:"skip"`
	Where   map[string]any `yaml:"where"`
	Include []string       `yaml:"include"`
	Fields  map[string]string `yaml:"fields"`
}

type Config struct {
	Classes map[string]ClassConfig `yaml:"classes"`
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
		cc, ok := c.Classes[k]
		if !ok {
			return fmt.Errorf("missing required class %q", k)
		}
		if cc.Name == "" {
			return fmt.Errorf("class %q: name is required", k)
		}
	}
	return nil
}

// Field returns the Parse field name for a given internal name, or "" if not
// configured.
func (c ClassConfig) Field(internal string) string {
	if c.Fields == nil {
		return ""
	}
	return c.Fields[internal]
}

// MustField is for fields the caller treats as mandatory; missing → error.
func (c ClassConfig) MustField(internal string) (string, error) {
	f := c.Field(internal)
	if f == "" {
		return "", errors.New("missing field mapping for " + internal)
	}
	return f, nil
}
