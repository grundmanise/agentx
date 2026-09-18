package main

import (
	"context"
	"os"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/cli"
)

func main() {
	env := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(cli.Run(context.Background(), os.Args[1:], env, os.Stdin, os.Stdout, os.Stderr))
}
