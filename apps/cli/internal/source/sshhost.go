package source

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/proc"
)

// sshHostWait bounds one ssh -G. It reads configuration files and opens no
// connection, so it answers at once; the bound is for a configuration whose
// Match exec runs a command of the user's that hangs.
const sshHostWait = 5 * time.Second

// SSHHosts returns the resolve function SameRepository takes: the host
// name the user's SSH configuration gives a host, as ssh -G prints it, so
// that an alias such as github-work in ~/.ssh/config compares as the
// github.com it stands for. ssh runs in env, the user's environment, found
// on its PATH; without one, as for a host ssh cannot read or one that
// would read as an option, every host resolves to itself. Each host is
// asked about once per resolve function, and only when SameRepository
// needs it; it is not safe for concurrent use.
func SSHHosts(ctx context.Context, env map[string]string) func(host string) string {
	known := map[string]string{}
	ssh := gitx.FindProgram(env, "ssh")
	vars := make([]string, 0, len(env))
	for k, v := range env {
		vars = append(vars, k+"="+v)
	}
	sort.Strings(vars)
	return func(host string) string {
		if ssh == "" || host == "" || strings.HasPrefix(host, "-") {
			return host
		}
		if name, ok := known[host]; ok {
			return name
		}
		name := host
		// No terminal, no input: ssh -G asks for nothing, and a command a
		// Match exec runs gets nothing to read either.
		var out strings.Builder
		res, err := proc.Run(ctx, proc.Command{
			Path: ssh, Args: []string{"-G", "--", host}, Env: vars,
			Deadline: sshHostWait, Grace: time.Second,
		}, func(stream string, text []byte) {
			if stream == proc.Stdout {
				out.Write(text)
				out.WriteByte('\n')
			}
		})
		if err == nil && res.ExitCode != nil && *res.ExitCode == 0 {
			if resolved := sshHostname(out.String()); resolved != "" {
				name = resolved
			}
		}
		known[host] = name
		return name
	}
}

// sshHostname reads the hostname line of what ssh -G prints, "" when there
// is none. ssh prints every keyword in lower case, one per line, followed
// by its value. Pure.
func sshHostname(out string) string {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && key == "hostname" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
