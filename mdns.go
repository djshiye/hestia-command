package main

// A small mDNS browser for the _ewelink._tcp service. It listens on the
// multicast group on every usable interface and also sends "legacy unicast"
// queries from a random port, so it still works when the Windows firewall
// blocks unsolicited inbound traffic on UDP 5353.

import (
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

const ewelinkService = "_ewelink._tcp.local."

// Device IDs are 10 hex characters. Anything else announced on the network
// is ignored rather than trusted.
var reAnnouncedID = regexp.MustCompile(`^[0-9a-f]{10}$`)

// cleanLabel keeps a network-supplied label to printable ASCII and a sane
// length, so it can never smuggle control characters into the log or UI.
func cleanLabel(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= max {
			break
		}
	}
	return b.String()
}

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

type Discovered struct {
	ID        string            `json:"id"`
	Instance  string            `json:"instance"`
	IP        string            `json:"ip"`
	Port      int               `json:"port"`
	Type      string            `json:"type"`
	APIVer    string            `json:"apivers"`
	Encrypted bool              `json:"encrypted"`
	Seq       string            `json:"seq"`
	TXT       map[string]string `json:"-"`
	FirstSeen time.Time         `json:"firstSeen"`
	LastSeen  time.Time         `json:"lastSeen"`
	Goodbye   bool              `json:"goodbye"`
}

// payload returns the encrypted (or plain) state blob and its IV.
func (d *Discovered) payload() (data, iv string) {
	var b strings.Builder
	for i := 1; i <= 4; i++ {
		b.WriteString(d.TXT["data"+strconv.Itoa(i)])
	}
	return b.String(), d.TXT["iv"]
}

type Browser struct {
	mu        sync.Mutex
	byInst    map[string]*Discovered
	listeners []*net.UDPConn
	ifaces    []net.Interface
	uconn     *ipv4.PacketConn
	packets   int
	ewPackets int
	notes     []string
	onChange  func(id string)
}

func NewBrowser(onChange func(id string)) *Browser {
	return &Browser{byInst: map[string]*Discovered{}, onChange: onChange}
}

func usableInterfaces() []net.Interface {
	all, _ := net.Interfaces()
	var out []net.Interface
	for _, ifc := range all {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ifc)
				break
			}
		}
	}
	return out
}

func (b *Browser) Start() {
	b.ifaces = usableInterfaces()
	if len(b.ifaces) == 0 {
		b.note("No active network adapter with an IPv4 address was found.")
	}
	for i := range b.ifaces {
		ifc := b.ifaces[i]
		c, err := net.ListenMulticastUDP("udp4", &ifc, mdnsGroup)
		if err != nil {
			b.note("Could not listen for announcements on " + ifc.Name + ": " + err.Error())
			continue
		}
		b.listeners = append(b.listeners, c)
		go b.readLoop(c)
	}
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		b.note("Could not open a query socket: " + err.Error())
	} else {
		b.uconn = ipv4.NewPacketConn(uc)
		_ = b.uconn.SetMulticastTTL(255)
		go b.readLoop(uc)
	}
}

func (b *Browser) note(s string) {
	log.Println("mdns:", s)
	b.mu.Lock()
	b.notes = append(b.notes, s)
	b.mu.Unlock()
}

func (b *Browser) Interfaces() []string {
	var out []string
	for _, ifc := range b.ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ifc.Name+" ("+ipn.IP.String()+")")
			}
		}
	}
	return out
}

func (b *Browser) Stats() (packets, ewelink int, notes []string, listening int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.packets, b.ewPackets, append([]string{}, b.notes...), len(b.listeners)
}

func buildQuery(names map[string]uint16, unicastResponse bool) []byte {
	m := new(dns.Msg)
	m.Id = 0
	m.RecursionDesired = false
	for name, qtype := range names {
		q := dns.Question{Name: name, Qtype: qtype, Qclass: dns.ClassINET}
		if unicastResponse {
			q.Qclass |= 1 << 15
		}
		m.Question = append(m.Question, q)
	}
	buf, _ := m.Pack()
	return buf
}

// Query asks every device on the network to announce itself.
func (b *Browser) Query() {
	names := map[string]uint16{ewelinkService: dns.TypePTR}
	b.mu.Lock()
	for inst, d := range b.byInst {
		if d.IP == "" || d.Port == 0 {
			names[inst] = dns.TypeANY
		}
	}
	b.mu.Unlock()
	multi := buildQuery(names, false)
	uni := buildQuery(names, true)
	for _, c := range b.listeners {
		_, _ = c.WriteToUDP(multi, mdnsGroup)
	}
	if b.uconn != nil {
		for i := range b.ifaces {
			if err := b.uconn.SetMulticastInterface(&b.ifaces[i]); err == nil {
				_, _ = b.uconn.WriteTo(uni, nil, mdnsGroup)
			}
		}
	}
}

func (b *Browser) readLoop(c *net.UDPConn) {
	buf := make([]byte, 9000)
	for {
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			if strings.Contains(err.Error(), "closed") {
				return
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			continue
		}
		b.handle(msg, src.IP)
	}
}

func (b *Browser) handle(msg *dns.Msg, src net.IP) {
	b.mu.Lock()
	b.packets++
	b.mu.Unlock()
	if !msg.Response || !isLocalIP(src) {
		return
	}
	rrs := append(append(append([]dns.RR{}, msg.Answer...), msg.Ns...), msg.Extra...)
	hosts := map[string]net.IP{}
	type partial struct {
		srv     *dns.SRV
		txt     *dns.TXT
		goodbye bool
	}
	parts := map[string]*partial{}
	get := func(name string) *partial {
		name = strings.ToLower(name)
		if parts[name] == nil {
			parts[name] = &partial{}
		}
		return parts[name]
	}
	isOurs := func(name string) bool {
		return strings.HasSuffix(strings.ToLower(name), "."+ewelinkService)
	}
	for _, rr := range rrs {
		switch r := rr.(type) {
		case *dns.PTR:
			if strings.EqualFold(r.Hdr.Name, ewelinkService) && isOurs(r.Ptr) {
				p := get(r.Ptr)
				if r.Hdr.Ttl == 0 {
					p.goodbye = true
				}
			}
		case *dns.SRV:
			if isOurs(r.Hdr.Name) {
				get(r.Hdr.Name).srv = r
			}
		case *dns.TXT:
			if isOurs(r.Hdr.Name) {
				get(r.Hdr.Name).txt = r
			}
		case *dns.A:
			hosts[strings.ToLower(r.Hdr.Name)] = r.A
		}
	}
	if len(parts) == 0 {
		return
	}
	b.mu.Lock()
	b.ewPackets++
	var changed []string
	now := time.Now()
	for inst, p := range parts {
		d := b.byInst[inst]
		if d == nil {
			d = &Discovered{Instance: inst, FirstSeen: now, TXT: map[string]string{}}
			b.byInst[inst] = d
		}
		before := d.Seq + "|" + d.IP + "|" + strconv.FormatBool(d.Goodbye)
		d.LastSeen = now
		d.Goodbye = p.goodbye
		if p.txt != nil {
			kv := map[string]string{}
			for _, s := range p.txt.Txt {
				if k, v, ok := strings.Cut(s, "="); ok {
					kv[strings.ToLower(cleanLabel(k, 32))] = cleanLabel(v, 512)
				}
			}
			d.TXT = kv
			d.ID = strings.ToLower(kv["id"])
			d.Type = cleanLabel(kv["type"], 32)
			d.APIVer = cleanLabel(kv["apivers"], 16)
			d.Seq = cleanLabel(kv["seq"], 16)
			d.Encrypted = strings.EqualFold(kv["encrypt"], "true")
		}
		if d.ID == "" {
			label := strings.SplitN(inst, ".", 2)[0]
			d.ID = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(label, "ewelink_"), "eWeLink_"))
		}
		if !reAnnouncedID.MatchString(d.ID) {
			delete(b.byInst, inst)
			continue
		}
		if p.srv != nil {
			d.Port = int(p.srv.Port)
			if ip := hosts[strings.ToLower(p.srv.Target)]; ip != nil && isLocalIP(ip) {
				d.IP = ip.String()
			}
		}
		if d.IP == "" {
			d.IP = src.String()
		}
		if before != d.Seq+"|"+d.IP+"|"+strconv.FormatBool(d.Goodbye) {
			changed = append(changed, d.ID)
		}
	}
	b.mu.Unlock()
	for _, id := range changed {
		if b.onChange != nil {
			b.onChange(id)
		}
	}
}

// Devices returns a copy of every device seen so far.
func (b *Browser) Devices() []Discovered {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[string]bool{}
	var out []Discovered
	for _, d := range b.byInst {
		if d.ID == "" || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		cp := *d
		cp.TXT = map[string]string{}
		for k, v := range d.TXT {
			cp.TXT[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func (b *Browser) Device(id string) (Discovered, bool) {
	for _, d := range b.Devices() {
		if d.ID == id {
			return d, true
		}
	}
	return Discovered{}, false
}
