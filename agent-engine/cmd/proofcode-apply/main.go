package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/artifact"
)

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "ProofCode control plane URL")
	taskID := flag.String("task", "", "task ID")
	repository := flag.String("repo", "", "path to a clean local Git checkout")
	kind := flag.String("kind", "checkpoint", "checkpoint or recovery")
	flag.Parse()
	if *taskID == "" || *repository == "" {
		log.Fatal("-task and -repo are required")
	}
	token := os.Getenv("PROOFCODE_AUTH_TOKEN")
	if token == "" {
		log.Fatal("PROOFCODE_AUTH_TOKEN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := artifact.Fetch(ctx, &http.Client{Timeout: 15 * time.Second}, *baseURL, token, *taskID, *kind)
	if err != nil {
		log.Fatal(err)
	}
	if err := artifact.Apply(ctx, *repository, value); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Applied %s artifact for task %s as unstaged changes in %s\n", *kind, *taskID, *repository)
}
