// Package config is the single definition of every user-configurable multimux
// setting: its name, type, default, and validation. The `multimux config` CLI
// and the settings API both go through it, so they cannot disagree about what
// a setting is called, what values it accepts, or what it does when unset.
//
// Values live in the store's `settings` table under the underscored form of
// the key name (confirm-terminate -> confirm_terminate).
package config

import (
	"fmt"
	"strings"

	"github.com/jontuk/multimux/internal/store"
)

// Kind is a setting's value type. Only KindBool exists today; the type is here
// so adding a string or int setting does not mean reworking every caller.
type Kind int

const (
	KindBool Kind = iota
)

// Key describes one user-configurable setting.
type Key struct {
	Name    string // CLI name, e.g. "confirm-terminate"
	Kind    Kind
	Default string // the effective value when no row is stored
	Help    string // one line, shown by `multimux config list`
}

// ConfirmTerminate asks the browser to confirm before terminating a session.
const ConfirmTerminate = "confirm-terminate"

// Keys is every user-configurable setting, in the order `config list` prints.
var Keys = []Key{{
	Name:    ConfirmTerminate,
	Kind:    KindBool,
	Default: "false",
	Help:    "ask for confirmation before terminating a session",
}}

// Lookup finds a key by its CLI name.
func Lookup(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// storeKey is the settings-table spelling of a CLI key name.
func storeKey(name string) string { return strings.ReplaceAll(name, "-", "_") }

func unknown(name string) error {
	names := make([]string, 0, len(Keys))
	for _, k := range Keys {
		names = append(names, k.Name)
	}
	return fmt.Errorf("unknown setting %q (known settings: %s)", name, strings.Join(names, ", "))
}

// Normalize validates raw and returns the canonical stored form.
func Normalize(k Key, raw string) (string, error) {
	switch k.Kind {
	case KindBool:
		if raw != "true" && raw != "false" {
			return "", fmt.Errorf("%s takes true or false, got %q", k.Name, raw)
		}
		return raw, nil
	default:
		return "", fmt.Errorf("setting %s has an unsupported kind", k.Name)
	}
}

// Get returns the effective value: the stored row, or the key's default when
// nothing has been stored.
func Get(st *store.Store, name string) (string, error) {
	k, ok := Lookup(name)
	if !ok {
		return "", unknown(name)
	}
	v, err := st.GetSetting(storeKey(name))
	if err != nil {
		return "", err
	}
	if v == "" {
		return k.Default, nil
	}
	return v, nil
}

// Set validates value and stores it.
func Set(st *store.Store, name, value string) error {
	k, ok := Lookup(name)
	if !ok {
		return unknown(name)
	}
	v, err := Normalize(k, value)
	if err != nil {
		return err
	}
	return st.SetSetting(storeKey(name), v)
}

// Bool is Get for a KindBool setting.
func Bool(st *store.Store, name string) (bool, error) {
	v, err := Get(st, name)
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

// IsDefault reports whether name has no stored row, so `config list` can say so.
func IsDefault(st *store.Store, name string) (bool, error) {
	if _, ok := Lookup(name); !ok {
		return false, unknown(name)
	}
	v, err := st.GetSetting(storeKey(name))
	if err != nil {
		return false, err
	}
	return v == "", nil
}
