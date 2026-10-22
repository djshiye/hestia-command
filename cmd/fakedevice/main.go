// fakedevice simulates a Sonoff switch running stock eWeLink firmware in LAN
// mode, for testing Hestia without real hardware.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var (
	id        = flag.String("id", "1000abc123", "device id")
	key       = flag.String("key", "7f0a3b1c-2d4e-4f60-8a9b-0c1d2e3f4a5b", "device key")
	ifName    = flag.String("iface", "eth0", "interface")
	setupAddr = flag.String("setup", "127.0.0.1:8090", "setup-mode HTTP address")
	port      = flag.Int("port", 8081, "LAN API port")

	mu    sync.Mutex
	state = "off"
	seq   = 1
)

var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

func k() []byte { s := md5.Sum([]byte(*key)); return s[:] }

func enc(plain []byte) (string, string) {
	b, _ := aes.NewCipher(k())
	iv := make([]byte, 16)
	rand.Read(iv)
	p := 16 - len(plain)%16
	plain = append(plain, bytes.Repeat([]byte{byte(p)}, p)...)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, plain)
	return base64.StdEncoding.EncodeToString(out), base64.StdEncoding.EncodeToString(iv)
}

func dec(data, ivs string) ([]byte, error) {
	ct, _ := base64.StdEncoding.DecodeString(data)
	iv, _ := base64.StdEncoding.DecodeString(ivs)
	if len(ct) == 0 || len(ct)%16 != 0 || len(iv) != 16 {
		return nil, fmt.Errorf("bad")
	}
	b, _ := aes.NewCipher(k())
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(b, iv).CryptBlocks(out, ct)
	p := int(out[len(out)-1])
	if p < 1 || p > 16 {
		return nil, fmt.Errorf("bad pad")
	}
	out = out[:len(out)-p]
	if !json.Valid(out) {
		return nil, fmt.Errorf("bad json")
	}
	return out, nil
}

func myIP() net.IP {
	ifc, _ := net.InterfaceByName(*ifName)
	as, _ := ifc.Addrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.To4()
		}
	}
	return nil
}

func records() []dns.RR {
	mu.Lock()
	plain, _ := json.Marshal(map[string]any{"switch": state, "startup": "stay", "pulse": "off", "sledOnline": "on"})
	s := seq
	mu.Unlock()
	data, iv := enc(plain)
	inst := "eWeLink_" + *id + "._ewelink._tcp.local."
	host := "eWeLink_" + *id + ".local."
	h := func(name string, t uint16, ttl uint32) dns.RR_Header {
		return dns.RR_Header{Name: name, Rrtype: t, Class: dns.ClassINET | 1<<15, Ttl: ttl}
	}
	ptrH := h("_ewelink._tcp.local.", dns.TypePTR, 4500)
	ptrH.Class = dns.ClassINET
	return []dns.RR{
		&dns.PTR{Hdr: ptrH, Ptr: inst},
		&dns.SRV{Hdr: h(inst, dns.TypeSRV, 120), Port: uint16(*port), Target: host},
		&dns.TXT{Hdr: h(inst, dns.TypeTXT, 4500), Txt: []string{"txtvers=1", "id=" + *id, "type=plug", "apivers=1", "seq=" + strconv.Itoa(s), "encrypt=true", "iv=" + iv, "data1=" + data}},
		&dns.A{Hdr: h(host, dns.TypeA, 120), A: myIP()},
	}
}

func announce(c *net.UDPConn) {
	m := new(dns.Msg)
	m.Response, m.Authoritative = true, true
	m.Answer = records()
	b, _ := m.Pack()
	c.WriteToUDP(b, group)
}

func main() {
	flag.Parse()
	ifc, err := net.InterfaceByName(*ifName)
	if err != nil {
		log.Fatal(err)
	}
	c, err := net.ListenMulticastUDP("udp4", ifc, group)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		buf := make([]byte, 9000)
		for {
			n, src, err := c.ReadFromUDP(buf)
			if err != nil {
				continue
			}
			q := new(dns.Msg)
			if q.Unpack(buf[:n]) != nil || q.Response {
				continue
			}
			match, unicast := false, src.Port != 5353
			for _, qq := range q.Question {
				ln := strings.ToLower(qq.Name)
				if ln == "_ewelink._tcp.local." || strings.HasPrefix(ln, "ewelink_"+*id) {
					match = true
				}
				if qq.Qclass&(1<<15) != 0 {
					unicast = true
				}
			}
			if !match {
				continue
			}
			r := new(dns.Msg)
			r.Response, r.Authoritative = true, true
			r.Answer = records()
			if src.Port != 5353 {
				r.Id, r.Question = q.Id, q.Question
			}
			b, _ := r.Pack()
			if unicast {
				c.WriteToUDP(b, src)
			} else {
				c.WriteToUDP(b, group)
			}
		}
	}()
	announce(c)

	lan := http.NewServeMux()
	lan.HandleFunc("POST /zeroconf/switch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Sequence, Deviceid, Iv, Data string
			Encrypt                      bool
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &in)
		plain, err := dec(in.Data, in.Iv)
		if err != nil || in.Deviceid != *id {
			log.Println("rejected command:", err)
			json.NewEncoder(w).Encode(map[string]any{"error": 401, "sequence": in.Sequence})
			return
		}
		var p struct{ Switch string }
		json.Unmarshal(plain, &p)
		mu.Lock()
		state = p.Switch
		seq++
		s := seq
		mu.Unlock()
		log.Println("switch ->", p.Switch)
		json.NewEncoder(w).Encode(map[string]any{"seq": s, "sequence": in.Sequence, "error": 0})
		go announce(c)
	})
	go http.ListenAndServe(":"+strconv.Itoa(*port), lan)

	setup := http.NewServeMux()
	setup.HandleFunc("GET /device", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"deviceid": *id, "apikey": *key, "accept": "post"})
	})
	go http.ListenAndServe(*setupAddr, setup)
	log.Printf("fake Sonoff %s on %s:%d", *id, myIP(), *port)
	for {
		time.Sleep(time.Hour)
	}
}
