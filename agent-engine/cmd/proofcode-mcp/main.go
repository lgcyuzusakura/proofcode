// proofcode-mcp makes ProofCode's workspace tools available to any MCP client.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/mcp"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/tool"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/workspace"
	"os"
	"os/signal"
	"strings"
)

func main() {
	root := flag.String("root", ".", "Workspace root")
	write := flag.Bool("allow-write", false, "Expose apply_patch; client is responsible for user approval")
	programs := flag.String("allow-programs", "", "Comma-separated command allowlist; empty disables execution")
	flag.Parse()
	ws, err := workspace.Open(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	registry := tool.NewRegistry(tool.ReadFile{Workspace: ws}, tool.ReadFiles{Workspace: ws}, tool.FileInfo{Workspace: ws}, tool.ListFiles{Workspace: ws}, tool.SearchCode{Workspace: ws}, tool.GitDiff{Workspace: ws}, tool.GitInspect{Workspace: ws})
	if *write {
		registry.Register(tool.ApplyPatch{Workspace: ws})
	}
	if *programs != "" {
		policy := tool.DefaultCommandPolicy()
		policy.AllowedPrograms = map[string]bool{}
		for _, name := range strings.Split(*programs, ",") {
			policy.AllowedPrograms[strings.TrimSpace(name)] = true
		}
		registry.Register(tool.RunCommand{Workspace: ws, Policy: policy})
	}
	registry = tool.WithDeterministicPolicy(registry)
	registry.Register(tool.Discover{Registry: registry})
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
