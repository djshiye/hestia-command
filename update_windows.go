//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func updateSupported() bool { return true }

// stageUpdate puts the verified new program in place of the running one.
// Windows lets a running program be renamed but not overwritten, so the old
// file is moved aside and deleted by the new copy on its first start.
func stageUpdate(newFile string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	old := self + ".old"
	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return "", errors.New("could not move the current program aside: " + err.Error())
	}
	if err := copyFile(newFile, self); err != nil {
		_ = os.Rename(old, self) // put things back
		return "", errors.New("could not write the new program: " + err.Error())
	}
	_ = os.Remove(newFile)
	return self, nil
}

// cleanupOldUpdate removes what the previous version left behind.
func cleanupOldUpdate() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	old := self + ".old"
	for i := 0; i < 10; i++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if dir, err := appDir(); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "updates"))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".part") || strings.HasSuffix(e.Name(), ".exe") {
				_ = os.Remove(filepath.Join(dir, "updates", e.Name()))
			}
		}
	}
}
