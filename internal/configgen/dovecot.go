package configgen

import (
	"os/exec"
	"regexp"
	"strconv"
)

// RenderDovecot produces every Dovecot file as name -> content. Rendering is
// pure: no filesystem access, no host effects. The output filenames are
// stable; only the source templates differ by Dovecot major version, so the
// installer's wiring is version-agnostic.
func RenderDovecot(d *Data) (map[string][]byte, error) {
	mainTmpl := "dovecot/templates/dovecot.conf.tmpl"
	sqlTmpl := "dovecot/templates/dovecot-sql.conf.ext.tmpl"
	if d.DovecotV24 {
		mainTmpl = "dovecot/templates/dovecot24.conf.tmpl"
		sqlTmpl = "dovecot/templates/dovecot-sql24.conf.ext.tmpl"
	}

	out := map[string][]byte{}
	for _, t := range []struct{ name, tmpl string }{
		{"dovecot.conf", mainTmpl},
		{"dovecot-sql.conf.ext", sqlTmpl},
	} {
		body, err := renderTemplate(t.tmpl, d)
		if err != nil {
			return nil, err
		}
		out[t.name] = body
	}
	return out, nil
}

var dovecotVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// DovecotIsV24Plus reports whether the installed Dovecot is 2.4 or newer. When
// Dovecot is not installed or its version can't be parsed, it returns false so
// callers fall back to the conservative 2.3 templates.
func DovecotIsV24Plus() bool {
	major, minor, ok := dovecotVersion()
	if !ok {
		return false
	}
	return major > 2 || (major == 2 && minor >= 4)
}

// dovecotVersion runs `dovecot --version` and returns the major/minor version.
func dovecotVersion() (major, minor int, ok bool) {
	out, err := exec.Command("dovecot", "--version").Output()
	if err != nil {
		return 0, 0, false
	}
	m := dovecotVersionRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}
