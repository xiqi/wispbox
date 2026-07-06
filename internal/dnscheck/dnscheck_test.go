package dnscheck

import (
	"context"
	"net"
	"strings"
	"testing"
)

func baseInputs() Inputs {
	return Inputs{
		Domain:       "example.com",
		MailHostname: "mail.example.com",
		ServerIPv4:   "192.0.2.10",
		DKIMSelector: "wisp",
	}
}

func recordByPurpose(t *testing.T, recs []Record, purpose string) Record {
	t.Helper()
	for _, r := range recs {
		if r.Purpose == purpose {
			return r
		}
	}
	t.Fatalf("no record with purpose %q in %+v", purpose, recs)
	return Record{}
}

func purposes(recs []Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Purpose
	}
	return out
}

func TestRequiredRecordsDirect(t *testing.T) {
	recs := RequiredRecords(baseInputs())

	want := []string{"a", "mx", "spf", "dmarc"}
	if got := purposes(recs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("purposes = %v, want %v", got, want)
	}

	checks := []struct {
		purpose, typ, name, value string
	}{
		{"a", "A", "mail.example.com", "192.0.2.10"},
		{"mx", "MX", "example.com", "10 mail.example.com"},
		{"spf", "TXT", "example.com", "v=spf1 mx ~all"},
		{"dmarc", "TXT", "_dmarc.example.com", "v=DMARC1; p=none; rua=mailto:postmaster@example.com"},
	}
	for _, c := range checks {
		r := recordByPurpose(t, recs, c.purpose)
		if r.Type != c.typ || r.Name != c.name || r.Value != c.value {
			t.Errorf("%s record = {Type:%q Name:%q Value:%q}, want {Type:%q Name:%q Value:%q}",
				c.purpose, r.Type, r.Name, r.Value, c.typ, c.name, c.value)
		}
		if r.Explanation == "" {
			t.Errorf("%s record has empty explanation", c.purpose)
		}
	}
}

func TestRequiredRecordsIPv6AndDKIM(t *testing.T) {
	in := baseInputs()
	in.ServerIPv6 = "2001:db8::10"
	in.DKIMTXTValue = "v=DKIM1; k=rsa; p=abc123"
	recs := RequiredRecords(in)

	want := []string{"a", "aaaa", "mx", "spf", "dkim", "dmarc"}
	if got := purposes(recs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("purposes = %v, want %v", got, want)
	}

	aaaa := recordByPurpose(t, recs, "aaaa")
	if aaaa.Type != "AAAA" || aaaa.Name != "mail.example.com" || aaaa.Value != "2001:db8::10" {
		t.Errorf("aaaa record = {Type:%q Name:%q Value:%q}, want AAAA mail.example.com 2001:db8::10",
			aaaa.Type, aaaa.Name, aaaa.Value)
	}

	dkim := recordByPurpose(t, recs, "dkim")
	if dkim.Type != "TXT" || dkim.Name != "wisp._domainkey.example.com" || dkim.Value != in.DKIMTXTValue {
		t.Errorf("dkim record = {Type:%q Name:%q Value:%q}, want TXT wisp._domainkey.example.com %q",
			dkim.Type, dkim.Name, dkim.Value, in.DKIMTXTValue)
	}
}

func TestRequiredRecordsRelaySPFInclude(t *testing.T) {
	in := baseInputs()
	in.SPFInclude = "include:relay.example.net"
	recs := RequiredRecords(in)

	spf := recordByPurpose(t, recs, "spf")
	want := "v=spf1 mx include:relay.example.net ~all"
	if spf.Value != want {
		t.Errorf("spf value = %q, want %q", spf.Value, want)
	}
}

func TestCheckerCheck(t *testing.T) {
	tests := []struct {
		name       string
		spfInclude string
		purpose    string
		seed       func(m *MockResolver)
		wantStatus string
		wantFound  string // empty means don't check Found
	}{
		{
			name:    "a ok",
			purpose: "a",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("192.0.2.10")}
			},
			wantStatus: StatusOK,
			wantFound:  "192.0.2.10",
		},
		{
			name:       "a missing when hostname does not resolve",
			purpose:    "a",
			seed:       func(m *MockResolver) {},
			wantStatus: StatusMissing,
		},
		{
			name:    "a missing when only AAAA exists",
			purpose: "a",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("2001:db8::10")}
			},
			wantStatus: StatusMissing,
		},
		{
			name:    "a mismatch when pointing elsewhere",
			purpose: "a",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("198.51.100.7")}
			},
			wantStatus: StatusMismatch,
			wantFound:  "198.51.100.7",
		},
		{
			name:    "mx ok with trailing dot and mixed case",
			purpose: "mx",
			seed: func(m *MockResolver) {
				m.MXs["example.com"] = []*net.MX{{Host: "MAIL.example.com.", Pref: 10}}
			},
			wantStatus: StatusOK,
			wantFound:  "10 mail.example.com",
		},
		{
			name:       "mx missing",
			purpose:    "mx",
			seed:       func(m *MockResolver) {},
			wantStatus: StatusMissing,
		},
		{
			name:    "mx mismatch when host differs",
			purpose: "mx",
			seed: func(m *MockResolver) {
				m.MXs["example.com"] = []*net.MX{{Host: "mx.other.example.", Pref: 5}}
			},
			wantStatus: StatusMismatch,
			wantFound:  "5 mx.other.example",
		},
		{
			name:    "spf ok",
			purpose: "spf",
			seed: func(m *MockResolver) {
				m.TXT["example.com"] = []string{"v=spf1 mx ~all"}
			},
			wantStatus: StatusOK,
			wantFound:  "v=spf1 mx ~all",
		},
		{
			name:       "spf missing when no txt",
			purpose:    "spf",
			seed:       func(m *MockResolver) {},
			wantStatus: StatusMissing,
		},
		{
			name:    "spf missing when txt is not spf",
			purpose: "spf",
			seed: func(m *MockResolver) {
				m.TXT["example.com"] = []string{"google-site-verification=xyz"}
			},
			wantStatus: StatusMissing,
		},
		{
			name:       "spf mismatch when relay include is absent",
			spfInclude: "include:relay.example.net",
			purpose:    "spf",
			seed: func(m *MockResolver) {
				m.TXT["example.com"] = []string{"v=spf1 mx ~all"}
			},
			wantStatus: StatusMismatch,
		},
		{
			name:    "spf mismatch when mx mechanism is absent",
			purpose: "spf",
			seed: func(m *MockResolver) {
				m.TXT["example.com"] = []string{"v=spf1 include:other.example ~all"}
			},
			wantStatus: StatusMismatch,
		},
		{
			name:       "spf ok in relay mode when include is present",
			spfInclude: "include:relay.example.net",
			purpose:    "spf",
			seed: func(m *MockResolver) {
				m.TXT["example.com"] = []string{"v=spf1 mx include:relay.example.net ~all"}
			},
			wantStatus: StatusOK,
		},
		{
			name:    "dkim ok",
			purpose: "dkim",
			seed: func(m *MockResolver) {
				m.TXT["wisp._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=abc123"}
			},
			wantStatus: StatusOK,
		},
		{
			name:    "dkim ok with provider whitespace differences",
			purpose: "dkim",
			seed: func(m *MockResolver) {
				m.TXT["wisp._domainkey.example.com"] = []string{"v=DKIM1;  k=rsa;  p=abc123"}
			},
			wantStatus: StatusOK,
		},
		{
			name:       "dkim missing",
			purpose:    "dkim",
			seed:       func(m *MockResolver) {},
			wantStatus: StatusMissing,
		},
		{
			name:    "dkim mismatch when key differs",
			purpose: "dkim",
			seed: func(m *MockResolver) {
				m.TXT["wisp._domainkey.example.com"] = []string{"v=DKIM1; k=rsa; p=SOMETHINGELSE"}
			},
			wantStatus: StatusMismatch,
		},
		{
			name:    "dmarc ok",
			purpose: "dmarc",
			seed: func(m *MockResolver) {
				m.TXT["_dmarc.example.com"] = []string{"v=DMARC1; p=none; rua=mailto:postmaster@example.com"}
			},
			wantStatus: StatusOK,
		},
		{
			name:       "dmarc missing",
			purpose:    "dmarc",
			seed:       func(m *MockResolver) {},
			wantStatus: StatusMissing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs()
			in.SPFInclude = tc.spfInclude
			in.DKIMTXTValue = "v=DKIM1; k=rsa; p=abc123"
			recs := RequiredRecords(in)

			m := NewMockResolver()
			tc.seed(m)
			got := NewChecker(m).Check(context.Background(), recs)

			r := recordByPurpose(t, got, tc.purpose)
			if r.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (found %q)", r.Status, tc.wantStatus, r.Found)
			}
			if tc.wantFound != "" && r.Found != tc.wantFound {
				t.Errorf("found = %q, want %q", r.Found, tc.wantFound)
			}
		})
	}
}

func TestCheckerCheckHappyDomainAllOK(t *testing.T) {
	in := baseInputs()
	in.DKIMTXTValue = "v=DKIM1; k=rsa; p=abc123"

	m := NewMockResolver()
	m.SeedHappyDomain(in.Domain, in.MailHostname, in.ServerIPv4, in.DKIMSelector, in.DKIMTXTValue)

	got := NewChecker(m).Check(context.Background(), RequiredRecords(in))
	for _, r := range got {
		if r.Status != StatusOK {
			t.Errorf("record %s (%s): status = %q, want %q (found %q)", r.Purpose, r.Name, r.Status, StatusOK, r.Found)
		}
	}
}

func TestCheckerCheckUnknownPurpose(t *testing.T) {
	got := NewChecker(NewMockResolver()).Check(context.Background(), []Record{{Purpose: "bogus"}})
	if len(got) != 1 || got[0].Status != StatusUnknown {
		t.Fatalf("got %+v, want single record with status %q", got, StatusUnknown)
	}
}

func TestPreflightHostname(t *testing.T) {
	tests := []struct {
		name      string
		seed      func(m *MockResolver)
		serverIPs []string
		wantErr   string // substring; empty means no error expected
	}{
		{
			name: "passes when A record matches server IP",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("192.0.2.10")}
			},
			serverIPs: []string{"192.0.2.10", ""},
		},
		{
			name: "passes when any resolved IP matches any server IP",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("198.51.100.7"), net.ParseIP("2001:db8::10")}
			},
			serverIPs: []string{"192.0.2.10", "2001:db8::10"},
		},
		{
			name:      "fails when hostname does not resolve",
			seed:      func(m *MockResolver) {},
			serverIPs: []string{"192.0.2.10"},
			wantErr:   "does not resolve yet",
		},
		{
			name: "fails when hostname points at a different server",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("198.51.100.7")}
			},
			serverIPs: []string{"192.0.2.10"},
			wantErr:   "resolves to 198.51.100.7, which is not this server",
		},
		{
			name: "fails when no server IP is configured",
			seed: func(m *MockResolver) {
				m.IPs["mail.example.com"] = []net.IP{net.ParseIP("192.0.2.10")}
			},
			serverIPs: []string{"", ""},
			wantErr:   "server public IP is not configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMockResolver()
			tc.seed(m)
			err := NewChecker(m).PreflightHostname(context.Background(), "mail.example.com", tc.serverIPs)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("PreflightHostname() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("PreflightHostname() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("PreflightHostname() = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if !strings.Contains(err.Error(), "mail.example.com") && tc.wantErr != "server public IP is not configured" {
				t.Errorf("error %q should mention the hostname", err.Error())
			}
		})
	}
}
