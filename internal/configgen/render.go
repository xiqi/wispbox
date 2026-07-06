package configgen

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/xiqi/wispbox/packaging"
)

// renderTemplate reads an embedded template, executes it against d, and
// returns the rendered bytes. It is the single template-rendering primitive
// shared by the Postfix, Dovecot, and OpenDKIM renderers.
func renderTemplate(name string, d *Data) ([]byte, error) {
	raw, err := packaging.Templates.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}
