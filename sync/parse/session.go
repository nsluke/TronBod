package parse

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// FileSession persists the session token to a single file with mode 0600.
type FileSession struct{ Path string }

func (f FileSession) Load() (string, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (f FileSession) Save(token string) error {
	return os.WriteFile(f.Path, []byte(token), 0o600)
}

func (f FileSession) Clear() error {
	err := os.Remove(f.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// MemorySession is for tests.
type MemorySession struct{ Token string }

func (m *MemorySession) Load() (string, error) { return m.Token, nil }
func (m *MemorySession) Save(t string) error   { m.Token = t; return nil }
func (m *MemorySession) Clear() error          { m.Token = ""; return nil }
