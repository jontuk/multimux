package svc

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// InstalledEnv reads the environment baked into the unit that is currently
// installed for goos, PATH included, in the order the unit lists it. A missing
// unit yields a nil slice and no error; an unreadable or unparseable one is an
// error, because the whole point of reading it is to avoid silently falling
// back to defaults (see mergeEnv).
func InstalledEnv(goos string) ([]EnvVar, error) {
	path, err := unitPath(goos)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch goos {
	case "darwin":
		return parsePlistEnv(b)
	default:
		return parseSystemdEnv(b), nil
	}
}

// parsePlistEnv pulls the EnvironmentVariables dict out of a LaunchAgent
// plist. It walks tokens rather than unmarshalling into a struct because a
// plist dict is a flat alternating key/value sequence, which does not map onto
// Go fields. XML entity decoding is the decoder's job, so this is the exact
// inverse of the xmlEscape in renderEnv.
func parsePlistEnv(b []byte) ([]EnvVar, error) {
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	var (
		out []EnvVar
		// depth counts open <dict> elements; envDepth is the depth of the
		// environment dict once found, so a nested dict cannot end the scan.
		depth, envDepth int
		armed           bool // the next <dict> is the environment
		elem, text      string
		curKey          string
		wantValue       bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("svc: parsing installed unit: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elem, text = t.Name.Local, ""
			if t.Name.Local == "dict" {
				depth++
				if armed {
					armed, envDepth = false, depth
				}
			}
		case xml.CharData:
			if elem == "key" || elem == "string" {
				text += string(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "dict":
				if depth == envDepth {
					envDepth = 0
				}
				depth--
			case "key":
				if envDepth != 0 && depth == envDepth {
					curKey, wantValue = text, true
				} else {
					armed = text == "EnvironmentVariables"
				}
			case "string":
				if envDepth != 0 && depth == envDepth && wantValue {
					out = append(out, EnvVar{Key: curKey, Value: text})
					wantValue = false
				}
			}
			elem, text = "", ""
		}
	}
}

// parseSystemdEnv reads Environment="KEY=value" assignments back out of a
// systemd unit, undoing systemdEscape. A line this does not understand is
// skipped rather than failing the whole read: the unit may have been
// hand-edited, and dropping one unrecognised line is better than refusing to
// upgrade.
func parseSystemdEnv(b []byte) []EnvVar {
	var out []EnvVar
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "Environment=")
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(rest, `"`)
		rest = strings.TrimSuffix(rest, `"`)
		key, value, ok := strings.Cut(rest, "=")
		if !ok || key == "" {
			continue
		}
		out = append(out, EnvVar{Key: key, Value: systemdUnescape(value)})
	}
	return out
}

// systemdUnescape inverts systemdEscape. One left-to-right pass, so an escaped
// backslash cannot be re-read as the escape for whatever follows it.
func systemdUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		if s[i] == '%' && i+1 < len(s) && s[i+1] == '%' {
			i++
			b.WriteByte('%')
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// mergeEnv layers the installing shell's captured variables over the ones the
// installed unit already carries. Without this, `service upgrade` — which
// rewrites the unit from the current environment — run from a shell that
// happens not to export MULTIMUX_DATA_DIR would restart the daemon against the
// *default* data dir: a fresh database, no passkeys and a new CA, which
// presents as the install having lost everything.
//
// The current environment still wins where it sets a variable, so a
// deliberate `MULTIMUX_DATA_DIR=... multimux service install` remains the way
// to move the daemon. Clearing a captured variable therefore needs
// `service uninstall` first.
//
// PATH is deliberately not preserved this way: it is set in practically every
// shell, so preserving it would only ever apply in the odd case, and a
// reinstall must stay able to fix a PATH that no longer finds tmux.
func mergeEnv(prev, captured []EnvVar) []EnvVar {
	// capturedEnvVars fixes the rendered order, so a unit rewritten with no
	// change of environment is byte-identical however the values were sourced.
	out := make([]EnvVar, 0, len(capturedEnvVars))
	for _, k := range capturedEnvVars {
		if e, ok := lookupEnv(captured, k); ok {
			out = append(out, e)
			continue
		}
		if e, ok := lookupEnv(prev, k); ok {
			out = append(out, e)
		}
	}
	return out
}

func lookupEnv(env []EnvVar, key string) (EnvVar, bool) {
	for _, e := range env {
		if e.Key == key {
			return e, true
		}
	}
	return EnvVar{}, false
}
