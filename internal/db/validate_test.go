package db

import (
	"strings"
	"testing"
)

func TestValidateDomainName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "example.com", true},
		{"subdomain", "sub.example.com", true},
		{"multi tld", "example.co.uk", true},
		{"hyphenated", "foo-bar.com", true},
		{"digits", "ex4mple99.com", true},
		{"surrounding space trimmed", "  example.com  ", true},

		{"empty", "", false},
		{"uppercase", "EXAMPLE.COM", false},
		{"mixed case", "Example.com", false},
		{"url scheme", "https://example.com", false},
		{"url path", "example.com/mail", false},
		{"mail prefix", "mail.example.com", false},
		{"catch-all style", "@example.com", false},
		{"full address", "user@example.com", false},
		{"no tld", "localhost", false},
		{"trailing dot", "example.com.", false},
		{"leading dot", ".example.com", false},
		{"consecutive dots", "example..com", false},
		{"leading hyphen label", "-foo.com", false},
		{"trailing hyphen label", "foo-.com", false},
		{"numeric tld", "example.123", false},
		{"underscore", "foo_bar.com", false},
		{"space inside", "exa mple.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomainName(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("ValidateDomainName(%q) = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"mail hostname", "mail.example.com", true},
		{"bare domain", "example.com", true},
		{"uppercase is lowercased", "MAIL.EXAMPLE.COM", true},
		{"deep subdomain", "mx1.mail.example.co.uk", true},

		{"empty", "", false},
		{"single label", "mail", false},
		{"url", "https://mail.example.com", false},
		{"trailing dot", "mail.example.com.", false},
		{"underscore", "mail_1.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("ValidateHostname(%q) = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}

func TestDefaultMailHostname(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"example.com", "mail.example.com"},
		{"EXAMPLE.COM", "mail.example.com"},
		{"  example.org  ", "mail.example.org"},
	}
	for _, tt := range tests {
		if got := DefaultMailHostname(tt.in); got != tt.want {
			t.Errorf("DefaultMailHostname(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidateAdminUsername(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "admin", true},
		{"dotted", "ops.admin", true},
		{"hyphen", "server-admin", true},
		{"underscore", "server_admin", true},
		{"digits", "admin2", true},
		{"surrounding space trimmed", "  admin  ", true},

		{"empty", "", false},
		{"too short", "ab", false},
		{"uppercase", "Admin", false},
		{"email address", "admin@example.com", false},
		{"space", "server admin", false},
		{"leading punctuation", ".admin", false},
		{"trailing punctuation", "admin-", false},
		{"too long", "a" + strings.Repeat("b", 31) + "c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdminUsername(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("ValidateAdminUsername(%q) = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}

func TestValidateLocalPart(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "info", true},
		{"single char", "a", true},
		{"dotted", "first.last", true},
		{"plus tag", "user+tag", true},
		{"hyphen", "no-reply", true},
		{"underscore", "user_name", true},
		{"digits", "user123", true},
		{"max length 64", "a" + strings.Repeat("b", 62) + "c", true},

		{"empty", "", false},
		{"uppercase", "Info", false},
		{"leading dot", ".user", false},
		{"trailing dot", "user.", false},
		{"consecutive dots", "us..er", false},
		{"leading hyphen", "-user", false},
		{"contains at", "user@example.com", false},
		{"space", "user name", false},
		{"catch-all style", "@", false},
		{"too long 65", strings.Repeat("a", 65), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalPart(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("ValidateLocalPart(%q) = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"simple", "user@example.com", true},
		{"tagged", "first.last+tag@sub.example.co.uk", true},
		{"uppercase allowed", "USER@EXAMPLE.COM", true},
		{"surrounding space trimmed", "  user@example.com  ", true},

		{"empty", "", false},
		{"no at", "userexample.com", false},
		{"missing domain", "user@", false},
		{"catch-all style", "@example.com", false},
		{"display name", "User <user@example.com>", false},
		{"two addresses", "a@example.com, b@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.in)
			if (err == nil) != tt.ok {
				t.Errorf("ValidateEmail(%q) = %v, want ok=%v", tt.in, err, tt.ok)
			}
		})
	}
}
