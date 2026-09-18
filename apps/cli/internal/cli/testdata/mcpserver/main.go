// Command mcpserver is a tiny MCP server on stdio for the handshake tests.
// It answers initialize, tools/list (two tools over two pages), prompts/list
// (one prompt) and resources/list (one resource); every other method is
// unknown. Its behaviour comes from flags: --variant a|b picks a tool
// description (MCPSERVER_VARIANT in the environment is the default),
// --hang never answers, --exit exits right after initialize and --linger
// exits at once leaving a child that holds stdout open, as a launcher such
// as npx can. It prints its environment to stderr, as a real server may.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	variant := flag.String("variant", "", "a or b; defaults to MCPSERVER_VARIANT, then a")
	hang := flag.Bool("hang", false, "never answer")
	exit := flag.Bool("exit", false, "exit after initialize")
	linger := flag.Bool("linger", false, "exit leaving a child that holds stdout")
	flag.Parse()
	if *variant == "" {
		*variant = os.Getenv("MCPSERVER_VARIANT")
	}
	if *variant == "" {
		*variant = "a"
	}
	for _, kv := range os.Environ() {
		fmt.Fprintln(os.Stderr, kv)
	}
	if *hang {
		io.Copy(io.Discard, os.Stdin) // reads every request and answers none
		return
	}
	if *linger {
		child := exec.Command(os.Args[0], "--hang")
		child.Stdin, child.Stdout = os.Stdin, os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		return
	}
	out := bufio.NewWriter(os.Stdout)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var m message
		if err := json.Unmarshal(in.Bytes(), &m); err != nil || m.ID == nil {
			continue // a notification, or noise
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			Cursor          string `json:"cursor"`
		}
		json.Unmarshal(m.Params, &params)
		reply := message{JSONRPC: "2.0", ID: m.ID}
		switch m.Method {
		case "initialize":
			reply.Result = map[string]any{
				"protocolVersion": params.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}, "resources": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mcpserver", "version": "0"},
			}
		case "tools/list":
			if params.Cursor == "" {
				reply.Result = map[string]any{
					"tools": []any{map[string]any{
						"name":        "add",
						"description": "Add two numbers",
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}, "b": map[string]any{"type": "number"}}, "required": []string{"a", "b"}},
					}},
					"nextCursor": "page-2",
				}
			} else {
				description := "Echo the input back"
				if *variant == "b" {
					description += " (variant b)"
				}
				reply.Result = map[string]any{"tools": []any{map[string]any{
					"name":         "echo",
					"description":  description,
					"inputSchema":  map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
					"outputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
				}}}
			}
		case "prompts/list":
			reply.Result = map[string]any{"prompts": []any{map[string]any{"name": "summarize", "description": "Summarize a text"}}}
		case "resources/list":
			reply.Result = map[string]any{"resources": []any{map[string]any{"uri": "file:///notes.md", "name": "notes", "description": "The notes"}}}
		default:
			reply.Error = &rpcError{Code: -32601, Message: "method not found"}
		}
		b, _ := json.Marshal(reply)
		out.Write(append(b, '\n'))
		out.Flush()
		if *exit && m.Method == "initialize" {
			os.Exit(0)
		}
	}
}
