// Package dnscheck generates the DNS records a domain needs and verifies
// them against live DNS (or a mock resolver in development).
package dnscheck

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Resolver abstracts DNS lookups so tests and dev mode never hit the network.
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
	LookupMX(ctx context.Context, domain string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// NetResolver resolves against the system's DNS.
type NetResolver struct{ r net.Resolver }

func NewNetResolver() *NetResolver { return &NetResolver{} }

func (n *NetResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := n.r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func (n *NetResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return n.r.LookupMX(ctx, domain)
}

func (n *NetResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return n.r.LookupTXT(ctx, name)
}

// MockResolver serves canned answers for development and tests.
type MockResolver struct {
	IPs map[string][]net.IP
	MXs map[string][]*net.MX
	TXT map[string][]string
}

func NewMockResolver() *MockResolver {
	return &MockResolver{IPs: map[string][]net.IP{}, MXs: map[string][]*net.MX{}, TXT: map[string][]string{}}
}

var errNXDomain = fmt.Errorf("no such host")

func (m *MockResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	if ips, ok := m.IPs[strings.ToLower(host)]; ok {
		return ips, nil
	}
	return nil, errNXDomain
}

func (m *MockResolver) LookupMX(_ context.Context, domain string) ([]*net.MX, error) {
	if mxs, ok := m.MXs[strings.ToLower(domain)]; ok {
		return mxs, nil
	}
	return nil, errNXDomain
}

func (m *MockResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if txt, ok := m.TXT[strings.ToLower(name)]; ok {
		return txt, nil
	}
	return nil, errNXDomain
}

// SeedHappyDomain configures the mock so every record for the domain checks
// out — used by dev mode to exercise the green path in the UI.
func (m *MockResolver) SeedHappyDomain(domain, mailHostname, serverIP, dkimSelector, dkimValue string) {
	ip := net.ParseIP(serverIP)
	m.IPs[strings.ToLower(mailHostname)] = []net.IP{ip}
	m.MXs[strings.ToLower(domain)] = []*net.MX{{Host: mailHostname + ".", Pref: 10}}
	m.TXT[strings.ToLower(domain)] = []string{"v=spf1 mx ~all"}
	m.TXT[strings.ToLower(dkimSelector+"._domainkey."+domain)] = []string{dkimValue}
	m.TXT[strings.ToLower("_dmarc."+domain)] = []string{"v=DMARC1; p=quarantine; rua=mailto:postmaster@" + domain}
}

// RecordStatus values.
const (
	StatusOK       = "ok"
	StatusMissing  = "missing"
	StatusMismatch = "mismatch"
	StatusUnknown  = "unknown"
)

// Record is one DNS record wispbox asks the user to create.
type Record struct {
	Type        string `json:"type"` // A, AAAA, MX, TXT
	Name        string `json:"name"` // fully qualified, no trailing dot
	Value       string `json:"value"`
	Purpose     string `json:"purpose"` // short machine tag: a, mx, spf, dkim, dmarc
	Explanation string `json:"explanation"`
	Status      string `json:"status"`
	Found       string `json:"found,omitempty"`
}

// Inputs describes everything needed to compute a domain's record set.
type Inputs struct {
	Domain       string
	MailHostname string
	ServerIPv4   string
	ServerIPv6   string // optional
	DKIMSelector string
	DKIMTXTValue string // full "v=DKIM1; k=rsa; p=..." value; empty if key not yet generated
	// SPFInclude is an extra include mechanism required by the active relay
	// provider (e.g. "include:amazonses.com"); empty for direct sending.
	SPFInclude string
}

// RequiredRecords computes the record set for a domain.
func RequiredRecords(in Inputs) []Record {
	spf := "v=spf1 mx"
	if in.SPFInclude != "" {
		spf += " " + in.SPFInclude
	}
	spf += " ~all"

	recs := []Record{
		{
			Type: "A", Name: in.MailHostname, Value: in.ServerIPv4, Purpose: "a",
			Explanation: "Points your mail hostname at this server. Required for webmail, IMAP, SMTP, and certificate issuance.",
		},
		{
			Type: "MX", Name: in.Domain, Value: "10 " + in.MailHostname, Purpose: "mx",
			Explanation: "Tells the world to deliver mail for @" + in.Domain + " to " + in.MailHostname + ".",
		},
		{
			Type: "TXT", Name: in.Domain, Value: spf, Purpose: "spf",
			Explanation: "SPF lists the servers allowed to send mail for your domain. Receivers use it to reject forgeries.",
		},
	}
	if in.ServerIPv6 != "" {
		recs = append(recs, Record{
			Type: "AAAA", Name: in.MailHostname, Value: in.ServerIPv6, Purpose: "aaaa",
			Explanation: "IPv6 counterpart of the A record.",
		})
	}
	if in.DKIMTXTValue != "" {
		recs = append(recs, Record{
			Type: "TXT", Name: in.DKIMSelector + "._domainkey." + in.Domain, Value: in.DKIMTXTValue, Purpose: "dkim",
			Explanation: "DKIM lets receivers verify your messages were really sent by you and were not modified in transit.",
		})
	}
	recs = append(recs, Record{
		Type: "TXT", Name: "_dmarc." + in.Domain, Value: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + in.Domain, Purpose: "dmarc",
		Explanation: "DMARC tells receivers what to do with mail that fails SPF/DKIM and where to send reports.",
	})
	sortRecords(recs)
	return recs
}

func sortRecords(recs []Record) {
	order := map[string]int{"a": 0, "aaaa": 1, "mx": 2, "spf": 3, "dkim": 4, "dmarc": 5}
	sort.SliceStable(recs, func(i, j int) bool { return order[recs[i].Purpose] < order[recs[j].Purpose] })
}

// Checker verifies records against a Resolver.
type Checker struct {
	resolver Resolver
}

func NewChecker(r Resolver) *Checker { return &Checker{resolver: r} }

// Check fills in Status and Found on each record.
func (c *Checker) Check(ctx context.Context, recs []Record) []Record {
	out := make([]Record, len(recs))
	for i, r := range recs {
		out[i] = c.checkOne(ctx, r)
	}
	return out
}

func (c *Checker) checkOne(ctx context.Context, r Record) Record {
	switch r.Purpose {
	case "a", "aaaa":
		ips, err := c.resolver.LookupIP(ctx, r.Name)
		if err != nil {
			r.Status = StatusMissing
			return r
		}
		var found []string
		for _, ip := range ips {
			isV6 := ip.To4() == nil
			if (r.Purpose == "aaaa") != isV6 {
				continue
			}
			found = append(found, ip.String())
			if ip.String() == r.Value {
				r.Status = StatusOK
				r.Found = ip.String()
				return r
			}
		}
		if len(found) == 0 {
			r.Status = StatusMissing
			return r
		}
		r.Status = StatusMismatch
		r.Found = strings.Join(found, ", ")
	case "mx":
		mxs, err := c.resolver.LookupMX(ctx, r.Name)
		if err != nil || len(mxs) == 0 {
			r.Status = StatusMissing
			return r
		}
		wantHost := strings.TrimSuffix(strings.Fields(r.Value)[1], ".")
		var found []string
		for _, mx := range mxs {
			host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
			found = append(found, fmt.Sprintf("%d %s", mx.Pref, host))
			if host == wantHost {
				r.Status = StatusOK
				r.Found = fmt.Sprintf("%d %s", mx.Pref, host)
				return r
			}
		}
		r.Status = StatusMismatch
		r.Found = strings.Join(found, ", ")
	case "spf":
		r = checkTXTPrefix(ctx, c.resolver, r, "v=spf1")
		if r.Status == StatusOK {
			// SPF exists; also ensure the required include/mx mechanism is
			// there. Match whole space-separated mechanisms, not substrings, so
			// a bare "mx" requirement isn't falsely satisfied by, say,
			// "include:mx.example.net".
			present := map[string]bool{}
			for _, f := range strings.Fields(r.Found) {
				present[f] = true
			}
			for _, mech := range requiredSPFMechanisms(r.Value) {
				if !present[mech] {
					r.Status = StatusMismatch
					return r
				}
			}
		}
	case "dkim":
		r = checkTXTPrefix(ctx, c.resolver, r, "v=DKIM1")
		if r.Status == StatusOK && !txtEqualLoose(r.Found, r.Value) {
			r.Status = StatusMismatch
		}
	case "dmarc":
		r = checkTXTPrefix(ctx, c.resolver, r, "v=DMARC1")
	default:
		r.Status = StatusUnknown
	}
	return r
}

func requiredSPFMechanisms(want string) []string {
	var mechs []string
	for _, f := range strings.Fields(want) {
		if f == "mx" || strings.HasPrefix(f, "include:") {
			mechs = append(mechs, f)
		}
	}
	return mechs
}

func checkTXTPrefix(ctx context.Context, res Resolver, r Record, prefix string) Record {
	txts, err := res.LookupTXT(ctx, r.Name)
	if err != nil || len(txts) == 0 {
		r.Status = StatusMissing
		return r
	}
	for _, t := range txts {
		if strings.HasPrefix(strings.TrimSpace(t), prefix) {
			r.Status = StatusOK
			r.Found = t
			return r
		}
	}
	r.Status = StatusMissing
	return r
}

// txtEqualLoose compares TXT values ignoring whitespace differences that DNS
// providers introduce when splitting long strings.
func txtEqualLoose(a, b string) bool {
	clean := func(s string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(s, "\" \"", "")), "")
	}
	return clean(a) == clean(b)
}

// PreflightHostname verifies that hostname's A/AAAA records point at one of
// serverIPs. The certificate manager calls this before any ACME order to
// avoid burning Let's Encrypt rate limits on domains that cannot validate.
func (c *Checker) PreflightHostname(ctx context.Context, hostname string, serverIPs []string) error {
	ips, err := c.resolver.LookupIP(ctx, hostname)
	if err != nil {
		return fmt.Errorf("%s does not resolve yet — create its A record first", hostname)
	}
	want := map[string]bool{}
	for _, s := range serverIPs {
		if s != "" {
			want[s] = true
		}
	}
	if len(want) == 0 {
		return fmt.Errorf("server public IP is not configured; cannot verify DNS for %s", hostname)
	}
	for _, ip := range ips {
		if want[ip.String()] {
			return nil
		}
	}
	var got []string
	for _, ip := range ips {
		got = append(got, ip.String())
	}
	return fmt.Errorf("%s resolves to %s, which is not this server — fix the A/AAAA record before requesting a certificate (or, if this server is behind NAT or a load balancer, set its public IP under Settings)",
		hostname, strings.Join(got, ", "))
}
