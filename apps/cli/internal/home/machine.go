package home

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Derivations of the machine id.
const (
	DerivedFromPlatform = "platform" // HMAC over the platform id and the uid
	DerivedRandom       = "random"   // generated once and kept in machine.json
)

func machinePath(dir string) string { return filepath.Join(dir, "machine.json") }

// MachineID returns this machine's id and how it was derived. A stored random
// id wins over a platform id; without either, a random id is generated and
// stored. Set AGENTX_PLATFORM_ID in env to fix the platform id, or to empty to
// declare that there is none.
func MachineID(dir string, env map[string]string) (id, derivation string, err error) {
	path := machinePath(dir)
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		var m struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(b, &m); err != nil || m.ID == "" {
			return "", "", fmt.Errorf("parse %s: not a machine file", path)
		}
		return m.ID, DerivedRandom, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", "", err
	}
	if pid := platformID(env); pid != "" {
		mac := hmac.New(sha256.New, []byte("agentx-machine-id/v1"))
		fmt.Fprintf(mac, "%s\n%d", pid, os.Getuid())
		return hex.EncodeToString(mac.Sum(nil))[:32], DerivedFromPlatform, nil
	}
	id, err = ResetMachineID(dir)
	return id, DerivedRandom, err
}

// ResetMachineID stores a new random id in machine.json and returns it. From
// then on the machine id is random even where a platform id exists.
func ResetMachineID(dir string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return id, writeAtomic(machinePath(dir), []byte(`{"id": "`+id+`"}`+"\n"))
}

func platformID(env map[string]string) string {
	if id, ok := env["AGENTX_PLATFORM_ID"]; ok {
		return id
	}
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(b))
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if _, v, ok := strings.Cut(line, `"IOPlatformUUID" = "`); ok {
					return strings.TrimSuffix(strings.TrimSpace(v), `"`)
				}
			}
		}
	}
	return ""
}

// Hostname is the default machine label: AGENTX_HOSTNAME from env when set,
// else what the operating system reports.
func Hostname(env map[string]string) string {
	if h := env["AGENTX_HOSTNAME"]; h != "" {
		return h
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "machine"
}
