package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DeviceCfg struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	KeyEnc   string    `json:"key"` // device key, protected with Windows DPAPI
	Verified bool      `json:"verified"`
	Hotkey   string    `json:"hotkey,omitempty"`
	ManualIP string    `json:"manualIp,omitempty"`
	Added    time.Time `json:"added"`
}

type Config struct {
	Devices    []DeviceCfg `json:"devices"`
	Background bool        `json:"background"`
	TestPassed bool        `json:"testPassed"`
	LastTest   time.Time   `json:"lastTest"`
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func appDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Hestia")
	return dir, os.MkdirAll(dir, 0o700)
}

func OpenStore() (*Store, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(raw, &s.cfg)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	raw, _ := json.MarshalIndent(s.cfg, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Update runs fn with the config locked and saves the result.
func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.cfg); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.Devices = append([]DeviceCfg{}, s.cfg.Devices...)
	return c
}

func (c *Config) device(id string) *DeviceCfg {
	for i := range c.Devices {
		if c.Devices[i].ID == id {
			return &c.Devices[i]
		}
	}
	return nil
}

func (s *Store) DeviceKey(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.cfg.device(id)
	if d == nil || d.KeyEnc == "" {
		return "", false
	}
	k, err := unprotect(d.KeyEnc)
	if err != nil {
		return "", false
	}
	return k, true
}
