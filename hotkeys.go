package main

import (
	"strconv"
	"strings"
)

// hotkeyChoices are the shortcuts offered in the app, Ctrl+Alt plus a key.
func hotkeyChoices() []string {
	var out []string
	for i := 1; i <= 9; i++ {
		out = append(out, "Ctrl+Alt+"+strconv.Itoa(i))
	}
	out = append(out, "Ctrl+Alt+0")
	for i := 1; i <= 12; i++ {
		out = append(out, "Ctrl+Alt+F"+strconv.Itoa(i))
	}
	return out
}

// hotkeyVK maps a choice to its Windows virtual-key code.
func hotkeyVK(name string) (uint32, bool) {
	key, ok := strings.CutPrefix(name, "Ctrl+Alt+")
	if !ok {
		return 0, false
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		return uint32(key[0]), true
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(key, "F")); err == nil && strings.HasPrefix(key, "F") && n >= 1 && n <= 12 {
		return uint32(0x70 + n - 1), true
	}
	return 0, false
}
