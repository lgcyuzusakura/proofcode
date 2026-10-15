package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/source"
)

func (r *runner) prepareLocalSource(ctx context.Context, task taskMessage, destination string) error {
	if task.SourceSnapshotID != "" {
		if !taskIDPattern.MatchString(task.SourceSnapshotID) {
			return errors.New("sourceSnapshotId must be a UUID")
		}
		response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+task.TaskID+"/source?attempt="+strconv.Itoa(task.Attempt), nil)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode/100 != 2 {
			return fmt.Errorf("source download returned %s", response.Status)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
		if err != nil {
			return err
		}
		if len(data) > 16<<20 {
			return errors.New("source response exceeds limit")
		}
		var archive source.Archive
		if err = json.Unmarshal(data, &archive); err != nil {
			return err
		}
		if archive.ManifestHash != task.SourceManifestHash {
			return errors.New("source response differs from queued manifest")
		}
		if err = archive.Materialize(destination); err != nil {
			return err
		}
	} else if task.SourceKind == "LOCAL_FOLDER" {
		return errors.New("local folder task requires an immutable source snapshot")
	} else {
		if err := os.MkdirAll(destination, 0750); err != nil {
			return err
		}
	}
	// Only the private attempt repository is committed; never the user's branch.
	for _, args := range [][]string{{"init", "--initial-branch=main"}, {"config", "core.autocrlf", "false"}, {"add", "-f", "-A"}, {"-c", "user.name=ProofCode", "-c", "user.email=proofcode@local", "-c", "core.hooksPath=", "commit", "--allow-empty", "-m", "Private source snapshot"}} {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = destination
		if out, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("initialize private source: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
func (r *runner) saveSourceResult(ctx context.Context, task taskMessage, root string) error {
	if task.SourceKind == "REMOTE_REPOSITORY" {
		return nil
	}
	archive, err := source.Capture(ctx, root)
	if err != nil {
		return err
	}
	response, err := r.request(ctx, http.MethodPost, "/internal/tasks/"+task.TaskID+"/source-result?attempt="+strconv.Itoa(task.Attempt)+"&runnerId="+r.ID, archive)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("persist source result: %s: %s", response.Status, body)
	}
	return nil
}
