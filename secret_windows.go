//go:build windows

package main

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protect encrypts a secret with DPAPI so only this Windows user can read it.
func protect(plain string) (string, error) {
	in := windows.DataBlob{Size: uint32(len(plain)), Data: unsafe.SliceData([]byte(plain))}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, windows.StringToUTF16Ptr("Hestia device key"), nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return "dpapi:" + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func unprotect(enc string) (string, error) {
	if len(enc) < 6 || enc[:6] != "dpapi:" {
		return "", errNotProtected
	}
	raw, err := base64.StdEncoding.DecodeString(enc[6:])
	if err != nil || len(raw) == 0 {
		return "", errNotProtected
	}
	in := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
