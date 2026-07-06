package services

import "testing"

func TestNormalizeJournalService(t *testing.T) {
	tests := map[string]string{
		"postfix@-.service": "postfix",
		"postfix.service":   "postfix",
		"dovecot.service":   "dovecot",
		"wispboxd.service":  "wispboxd",
		"opendkim.service":  "opendkim",
		"something.service": "something",
		"something.timer":   "something.timer",
	}

	for unit, want := range tests {
		if got := normalizeJournalService(unit); got != want {
			t.Fatalf("normalizeJournalService(%q) = %q, want %q", unit, got, want)
		}
	}
}

func TestClipLogMessage(t *testing.T) {
	long := make([]byte, maxLogMessageBytes+32)
	for i := range long {
		long[i] = 'x'
	}
	got := clipLogMessage(string(long))
	if len(got) != maxLogMessageBytes+3 {
		t.Fatalf("len(clipLogMessage) = %d, want %d", len(got), maxLogMessageBytes+3)
	}
	if got[len(got)-3:] != "..." {
		t.Fatalf("clipLogMessage suffix = %q, want ...", got[len(got)-3:])
	}
}
