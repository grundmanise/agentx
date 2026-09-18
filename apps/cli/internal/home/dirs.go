// Package home locates agentx home and the other directories the CLI works in.
package home

import (
	"errors"
	"path/filepath"
)

// Dirs are the roots every command works under. Home is agentx home.
type Dirs struct {
	Home    string
	Library string
	Config  string // the XDG config home, where agent clients keep user-scope configuration
}

// Resolve reads AGENTX_HOME, AGENTX_LIBRARY, HOME and XDG_CONFIG_HOME from env.
// The process environment is never consulted.
func Resolve(env map[string]string) (Dirs, error) {
	user := env["HOME"]
	if user == "" {
		return Dirs{}, errors.New("HOME is not set")
	}
	d := Dirs{
		Home:    env["AGENTX_HOME"],
		Library: env["AGENTX_LIBRARY"],
		Config:  env["XDG_CONFIG_HOME"],
	}
	if d.Home == "" {
		d.Home = filepath.Join(user, ".agentx")
	}
	if d.Library == "" {
		d.Library = filepath.Join(user, ".agents", "skills")
	}
	if d.Config == "" {
		d.Config = filepath.Join(user, ".config")
	}
	return d, nil
}
