package main

// eWeLink LAN protocol, as spoken by stock Sonoff firmware.
//
// Commands are HTTP POSTs to http://<ip>:8081/zeroconf/<command>. The "data"
// field is AES-128-CBC encrypted with key = MD5(devicekey) and a fresh random
// IV, PKCS#7 padded, base64 encoded. State is read from the device's mDNS TXT
// records (data1..data4 + iv), encrypted the same way.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" // #nosec G501 -- required by the eWeLink LAN protocol
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// aesKey derives the AES key the way the Sonoff firmware does. MD5 is not
// used for integrity or authentication here, only as the vendor's key
// derivation, so a stronger hash would simply not talk to the device.
func aesKey(deviceKey string) []byte {
	sum := md5.Sum([]byte(deviceKey)) // #nosec G401 G501 -- required by the eWeLink LAN protocol
	return sum[:]
}

func encryptPayload(deviceKey string, plain []byte) (data, iv string, err error) {
	block, err := aes.NewCipher(aesKey(deviceKey))
	if err != nil {
		return "", "", err
	}
	ivb := make([]byte, aes.BlockSize)
	if _, err := rand.Read(ivb); err != nil {
		return "", "", err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, ivb).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), base64.StdEncoding.EncodeToString(ivb), nil
}

var errBadKey = errors.New("could not decrypt: wrong device key")

func decryptPayload(deviceKey, data, iv string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("bad data encoding: %w", err)
	}
	ivb, err := base64.StdEncoding.DecodeString(iv)
	if err != nil || len(ivb) != aes.BlockSize {
		return nil, errors.New("bad iv")
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errBadKey
	}
	block, err := aes.NewCipher(aesKey(deviceKey))
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, ivb).CryptBlocks(out, ct)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, errBadKey
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errBadKey
		}
	}
	out = out[:len(out)-pad]
	if !json.Valid(out) {
		return nil, errBadKey
	}
	return out, nil
}

var lanClient = &http.Client{
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		Proxy:             nil, // never send LAN commands through a proxy
		DisableKeepAlives: true,
		DialContext:       (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// sendCommand posts an encrypted command to the device and returns the
// device's error code (0 means success).
func sendCommand(ip string, port int, deviceID, deviceKey, command string, params any) error {
	if !isLocalIP(net.ParseIP(ip)) {
		return fmt.Errorf("refusing to contact non-local address %s", ip)
	}
	plain, err := json.Marshal(params)
	if err != nil {
		return err
	}
	msg := map[string]any{
		"sequence":   strconv.FormatInt(time.Now().UnixMilli(), 10),
		"deviceid":   deviceID,
		"selfApikey": "123",
	}
	if deviceKey == "" {
		// Devices that announce encrypt=false (Sonoff DIY mode) take plain data.
		msg["data"] = json.RawMessage(plain)
	} else {
		data, iv, err := encryptPayload(deviceKey, plain)
		if err != nil {
			return err
		}
		msg["encrypt"], msg["iv"], msg["data"] = true, iv, data
	}
	body, _ := json.Marshal(msg)
	if port == 0 {
		port = 8081
	}
	url := fmt.Sprintf("http://%s/zeroconf/%s", net.JoinHostPort(ip, strconv.Itoa(port)), command)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connection", "close")
		resp, err := lanClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("switch did not answer: %w", err)
			time.Sleep(400 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("switch replied HTTP %d", resp.StatusCode)
		}
		var r struct {
			Error *int `json:"error"`
		}
		if err := json.Unmarshal(raw, &r); err != nil || r.Error == nil {
			return errors.New("switch sent an unexpected reply")
		}
		switch *r.Error {
		case 0:
			return nil
		case 401:
			return errors.New("switch rejected the command (wrong key)")
		case 400:
			return errors.New("switch rejected the command format")
		default:
			return fmt.Errorf("switch returned error %d", *r.Error)
		}
	}
	return lastErr
}

// setupHost is the address a Sonoff device uses for its own hotspot while in
// pairing ("compatible") mode.
var setupHost = "10.10.7.1"

type setupInfo struct {
	DeviceID string `json:"deviceid"`
	APIKey   string `json:"apikey"`
}

// readSetupKey reads the device ID and device key from a device in setup mode.
func readSetupKey() (*setupInfo, error) {
	c := &http.Client{
		Timeout:       4 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Get("http://" + setupHost + "/device")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var info setupInfo
	if err := json.Unmarshal(raw, &info); err != nil || info.DeviceID == "" || info.APIKey == "" {
		return nil, errors.New("connected, but the reply did not contain a device key")
	}
	return &info, nil
}

// extraLocalNet is only set by development builds (see setup_dev.go).
var extraLocalNet *net.IPNet

func isLocalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if extraLocalNet != nil && extraLocalNet.Contains(ip) {
		return true
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback()
}
