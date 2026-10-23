package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func composeFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		directory = resolved
	}
	if err := os.WriteFile(filepath.Join(directory, "compose.yml"), []byte("name: proofcode\nservices:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return directory
}
func runtimeFixture(t *testing.T) (*App, string, *[][]string) {
	t.Helper()
	directory := composeFixture(t)
	app := &App{registryDir: t.TempDir()}
	if err := app.saveRuntimeDirectory(directory); err != nil {
		t.Fatal(err)
	}
	operations := [][]string{}
	app.runtimeDockerRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) == 6 && args[0] == "ps" {
			return "", nil
		}
		t.Fatalf("unexpected Docker request: %v", args)
		return "", nil
	}
	app.runtimeRunner = func(_ context.Context, got string, args ...string) (string, error) {
		if got != directory {
			t.Fatalf("wrong directory %s", got)
		}
		operations = append(operations, append([]string{}, args...))
		if reflect.DeepEqual(args, []string{"config", "--services"}) {
			return strings.Join(proofCodeComposeServices, "\n"), nil
		}
		if reflect.DeepEqual(args, []string{"ps", "--all", "--format", "json"}) {
			rows := []map[string]string{}
			for _, name := range proofCodeComposeServices {
				rows = append(rows, map[string]string{"Service": name, "State": "running", "Health": "healthy"})
			}
			value, _ := json.Marshal(rows)
			return string(value), nil
		}
		if reflect.DeepEqual(args, []string{"up", "-d", "--build"}) || reflect.DeepEqual(args, []string{"stop"}) {
			return "", nil
		}
		t.Fatalf("unexpected Compose request: %v", args)
		return "", nil
	}
	return app, directory, &operations
}
func TestCanonicalComposeDirectoryRequiresExactProofCodeServiceSet(t *testing.T) {
	directory := composeFixture(t)
	for _, value := range []struct {
		services string
		valid    bool
	}{
		{"web\nrunner\nredis\npostgres\ncontrol-plane\nartemis", true},
		{"postgres\nother-app", false}, {strings.Join(proofCodeComposeServices, "\n") + "\nweb", false},
	} {
		got, err := canonicalComposeDirectory(context.Background(), directory, func(context.Context, string, ...string) (string, error) { return value.services, nil })
		if (err == nil) != value.valid || (value.valid && got != directory) {
			t.Fatalf("%q: %s %v", value.services, got, err)
		}
	}
	if _, err := canonicalComposeDirectory(context.Background(), t.TempDir(), nil); err == nil {
		t.Fatal("missing compose accepted")
	}
}
func TestManageRuntimeOnlyStartsOrStopsSelectedProjectAndObservesStatus(t *testing.T) {
	app, directory, operations := runtimeFixture(t)
	started, err := app.ManageRuntime("start")
	if err != nil || !started.Configured || !started.Observed || !started.Running || started.Busy || started.Directory != directory {
		t.Fatalf("start: %+v %v", started, err)
	}
	original := app.runtimeRunner
	app.runtimeRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "ps" {
			*operations = append(*operations, args)
			return `[{"Service":"postgres","State":"exited"}]`, nil
		}
		return original(ctx, dir, args...)
	}
	stopped, err := app.ManageRuntime("stop")
	if err != nil || stopped.Running || !stopped.Observed {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	expected := [][]string{{"config", "--services"}, {"up", "-d", "--build"}, {"ps", "--all", "--format", "json"}, {"config", "--services"}, {"stop"}, {"ps", "--all", "--format", "json"}}
	if !reflect.DeepEqual(*operations, expected) {
		t.Fatalf("commands: %#v", *operations)
	}
	if _, err = app.ManageRuntime("down"); err == nil {
		t.Fatal("down accepted")
	}
}
func TestRuntimeConfigRestoresWithoutRunningDocker(t *testing.T) {
	app, directory, _ := runtimeFixture(t)
	restored := &App{registryDir: app.registryDir}
	state, err := restored.GetRuntimeManagement()
	if err != nil || !state.Configured || state.Directory != directory || state.Observed || state.Running {
		t.Fatalf("restored: %+v %v", state, err)
	}
}
func TestRuntimeRejectsChangedConfigAndUnknownServicesBeforeMutation(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "services", true: "hash"}[changed], func(t *testing.T) {
			app, directory, operations := runtimeFixture(t)
			if changed {
				if err := os.WriteFile(filepath.Join(directory, "compose.yml"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				app.runtimeRunner = func(context.Context, string, ...string) (string, error) { return "other-app", nil }
			}
			if _, err := app.ManageRuntime("start"); err == nil {
				t.Fatal("unsafe config accepted")
			}
			if len(*operations) != 0 {
				t.Fatalf("unexpected mutation %v", *operations)
			}
		})
	}
}
func TestRuntimeOwnershipIncludesStoppedOrphanAndForeignContainers(t *testing.T) {
	for _, issue := range []string{"none", "directory", "config", "orphan", "missing", "malformed", "extra"} {
		t.Run(issue, func(t *testing.T) {
			app, directory, operations := runtimeFixture(t)
			app.runtimeDockerRunner = func(_ context.Context, args ...string) (string, error) {
				if args[0] == "ps" {
					return "abcdef012345", nil
				}
				labels := map[string]string{"com.docker.compose.project": "proofcode", "com.docker.compose.service": "postgres", "com.docker.compose.project.working_dir": directory, "com.docker.compose.project.config_files": filepath.Join(directory, "compose.yml")}
				switch issue {
				case "directory":
					labels["com.docker.compose.project.working_dir"] = t.TempDir()
				case "config":
					labels["com.docker.compose.project.config_files"] += ",other.yml"
				case "orphan":
					labels["com.docker.compose.service"] = "old-web"
				case "missing":
					delete(labels, "com.docker.compose.project.working_dir")
				case "malformed":
					return "broken secret", nil
				}
				data, _ := json.Marshal(labels)
				if issue == "extra" {
					return string(data) + "\n{}", nil
				}
				return string(data), nil
			}
			_, err := app.ManageRuntime("stop")
			if (err == nil) != (issue == "none") {
				t.Fatalf("ownership: %v", err)
			}
			for _, args := range *operations {
				if issue != "none" && args[0] != "config" {
					t.Fatalf("mutation before ownership: %v", args)
				}
			}
		})
	}
}
func TestRuntimeCommandSuccessDoesNotImplyReadiness(t *testing.T) {
	for _, output := range []string{`[]`, `{"Service":"postgres","State":"running","Health":"starting"}`, `[{"Service":"postgres","State":"running","Health":"unhealthy"}]`} {
		app, _, _ := runtimeFixture(t)
		original := app.runtimeRunner
		app.runtimeRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
			if args[0] == "ps" {
				return output, nil
			}
			return original(ctx, dir, args...)
		}
		state, err := app.ManageRuntime("start")
		if err != nil || state.Running || !state.Observed || !strings.Contains(state.Message, "尚未就绪") {
			t.Fatalf("readiness: %+v %v", state, err)
		}
	}
}
func TestRuntimeRejectsConcurrentOperationsAndCancelsOnShutdown(t *testing.T) {
	app, _, _ := runtimeFixture(t)
	original := app.runtimeRunner
	entered := make(chan struct{})
	done := make(chan error, 1)
	app.runtimeRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "up" {
			close(entered)
			<-ctx.Done()
			return "", ctx.Err()
		}
		return original(ctx, dir, args...)
	}
	go func() { _, err := app.ManageRuntime("start"); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("start never entered")
	}
	state, err := app.GetRuntimeManagement()
	if err != nil || !state.Busy || state.Phase != "starting" {
		t.Fatalf("busy state: %+v %v", state, err)
	}
	for _, invoke := range []func() (RuntimeManagement, error){func() (RuntimeManagement, error) { return app.ManageRuntime("stop") }, app.RefreshRuntimeManagement, app.SelectRuntimeDirectory} {
		if _, err := invoke(); err == nil || !strings.Contains(err.Error(), "正在进行") {
			t.Fatalf("did not reject concurrent call: %v", err)
		}
	}
	app.closeRuntimeOperations()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "关闭") {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel")
	}
	state, _ = app.GetRuntimeManagement()
	if state.Busy || state.Observed || state.Phase != "error" {
		t.Fatalf("terminal state: %+v", state)
	}
	if _, err := app.ManageRuntime("start"); err == nil {
		t.Fatal("started after shutdown")
	}
}
func TestRuntimeFailureIsRedactedAndRetainedAcrossRemount(t *testing.T) {
	app, _, _ := runtimeFixture(t)
	original := app.runtimeRunner
	app.runtimeRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
		if args[0] == "up" {
			return "", errors.New("private-secret-key")
		}
		return original(ctx, dir, args...)
	}
	_, err := app.ManageRuntime("start")
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("error: %v", err)
	}
	state, err := app.GetRuntimeManagement()
	if err != nil || state.Busy || state.Phase != "error" || strings.Contains(state.Message, "private-secret") {
		t.Fatalf("retained: %+v %v", state, err)
	}
}
func TestRuntimeOutputIsBoundedAndDrained(t *testing.T) {
	var output runtimeOutput
	data := []byte(strings.Repeat("x", (1<<20)+42))
	n, err := output.Write(data)
	if err != nil || n != len(data) || len(output.data) != 1<<20 || !output.overflow {
		t.Fatal("unbounded output")
	}
	n, err = output.Write([]byte("more"))
	if err != nil || n != 4 || len(output.data) != 1<<20 {
		t.Fatal("did not drain overflow")
	}
}
