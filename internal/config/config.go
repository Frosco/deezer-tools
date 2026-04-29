// Package config loads deezer-tools configuration from disk.
//
// The config file is TOML at ~/.config/deezer-tools/config.toml and must be
// 0600 since it holds the arl cookie, which is equivalent to a session token
// for the user's Deezer account.
package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Config holds the values read from the user's config file.
type Config struct {
	ARL string `toml:"arl"`
}

// Load reads and validates the config file at path.
// On unix it requires the file to be 0600.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s: %w", path, err)
		}
		return nil, fmt.Errorf("stat config %s: %w", path, err)
	}

	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf(
				"config file %s has insecure permissions %#o; run: chmod 600 %s",
				path, info.Mode().Perm(), path,
			)
		}
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if cfg.ARL == "" {
		return nil, fmt.Errorf("config %s is missing required field 'arl'", path)
	}

	return &cfg, nil
}
