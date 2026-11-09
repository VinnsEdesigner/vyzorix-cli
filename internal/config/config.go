
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// File is the on-disk config. Unset fields are omitted, so a later `login` does
// not have to carry a value it did not set.
type File struct {
	Server     string `toml:"server,omitempty"`
	Org        string `toml:"org,omitempty"`
	APIKey     string `toml:"api_key,omitempty"`
	Credential string `toml:"deploy_credential,omitempty"`
}

// Path returns the config file location. VYZORIX_CONFIG overrides it, which is
// what a test or a CI uses to keep its state out of the user's home.
func Path() (string, error) {
	if p := os.Getenv("VYZORIX_CONFIG"); p != "" {
		return p, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "vyzorix", "config.toml"), nil
}

// Load reads the config, returning an empty File when none exists yet. A missing
// file is not an error: it is the state before the first login.
func Load() (*File, error) {
	path, err := Path()
	if err != nil {
		return &File{}, nil
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}

	var f File
	if err := toml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}

	return &f, nil
}

// Save writes the config, creating the directory if needed, with the file mode
// 0600: it carries a credential, so it must not be group- or world-readable.
func Save(f *File) error {
	path, err := Path()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	raw, err := toml.Marshal(f)
	if err != nil {
		return err
	}

	return os.WriteFile(path, raw, 0o600)
}

// Remove deletes the config file. A file that is already gone is not an error:
// the post-condition the caller wants — no saved credential — already holds.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}
