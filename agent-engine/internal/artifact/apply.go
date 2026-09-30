package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxPatchBytes = 8 << 20

type Artifact struct {
	Kind     string `json:"kind"`
	Patch    string `json:"patch"`
	Metadata struct {
		Base string `json:"base"`
	} `json:"metadata"`
}

func Fetch(ctx context.Context, client *http.Client, baseURL, token, taskID, kind string) (Artifact, error) {
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return Artifact{}, fmt.Errorf("invalid control plane URL: %w", err)
	}
	if kind != "checkpoint" && kind != "recovery" {
		return Artifact{}, errors.New("artifact kind must be checkpoint or recovery")
	}
	if taskID == "" || strings.ContainsAny(taskID, "/?#") {
		return Artifact{}, errors.New("invalid task ID")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/tasks/" + url.PathEscape(taskID) + "/artifacts/" + kind
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Artifact{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Artifact{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return Artifact{}, fmt.Errorf("fetch artifact: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var value Artifact
	if err := json.NewDecoder(io.LimitReader(response.Body, maxPatchBytes*2)).Decode(&value); err != nil {
		return Artifact{}, fmt.Errorf("decode artifact: %w", err)
	}
	if value.Kind != kind || value.Metadata.Base == "" || value.Patch == "" || len(value.Patch) > maxPatchBytes {
		return Artifact{}, errors.New("artifact is missing a valid base revision or patch")
	}
	return value, nil
}

// Apply adds an artifact patch as unstaged changes in an explicitly selected,
// clean checkout at the exact revision the agent started from.
func Apply(ctx context.Context, repository string, value Artifact) error {
	if value.Metadata.Base == "" || value.Patch == "" || len(value.Patch) > maxPatchBytes {
		return errors.New("artifact is missing a valid base revision or patch")
	}
	abs, err := filepath.Abs(repository)
	if err != nil {
		return err
	}
	root, err := git(ctx, abs, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("open Git repository: %w", err)
	}
	root = strings.TrimSpace(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(abs)) {
		return fmt.Errorf("repository path must be the Git root: %s", root)
	}
	status, err := git(ctx, root, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("repository has local changes; apply to a clean checkout")
	}
	head, err := git(ctx, root, nil, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(head), strings.TrimSpace(value.Metadata.Base)) {
		return fmt.Errorf("base revision mismatch: repository is %s, artifact expects %s", strings.TrimSpace(head), value.Metadata.Base)
	}
	patch := []byte(value.Patch)
	if _, err := git(ctx, root, patch, "apply", "--check", "--binary", "--whitespace=error", "-"); err != nil {
		return fmt.Errorf("patch check failed: %w", err)
	}
	if _, err := git(ctx, root, patch, "apply", "--binary", "--whitespace=error", "-"); err != nil {
		return fmt.Errorf("patch apply failed: %w", err)
	}
	return nil
}

func git(ctx context.Context, dir string, input []byte, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
