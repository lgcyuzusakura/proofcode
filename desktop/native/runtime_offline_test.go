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

func offlineFixture(t *testing.T) (*App, string, *[][]string, *offlineRuntimeManifest, map[string]any) {
	t.Helper()
	app, directory, operations := runtimeFixture(t)
	hash, _ := composeFileHash(directory)
	manifest := &offlineRuntimeManifest{Version: 1, ComposeSHA256: hash, Images: map[string]string{}}
	services := map[string]any{}
	for index, name := range proofCodeComposeServices {
		id := "sha256:" + strings.Repeat(string(rune('a'+index)), 64)
		manifest.Images[name] = id
		services[name] = map[string]any{"image": id, "pull_policy": "never"}
	}
	config := map[string]any{"services": services}
	original := app.runtimeRunner
	app.runtimeRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
		if reflect.DeepEqual(args, []string{"config", "--format", "json"}) {
			data, _ := json.Marshal(config)
			return string(data), nil
		}
		if len(args) > 0 && args[0] == "up" {
			*operations = append(*operations, args)
			return "", nil
		}
		return original(ctx, dir, args...)
	}
	app.runtimeDockerRunner = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "ps" {
			return "", nil
		}
		if len(args) == 5 && reflect.DeepEqual(args[:4], []string{"image", "inspect", "--format", "{{.Id}}"}) {
			return args[4] + "\n", nil
		}
		t.Fatalf("unexpected Docker request %v", args)
		return "", nil
	}
	return app, directory, operations, manifest, config
}

func writeOfflineMarker(t *testing.T, directory string, manifest *offlineRuntimeManifest) {
	t.Helper()
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(directory, "proofcode-offline.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineStartNeverBuildsOrPullsAndStopPreservesData(t *testing.T) {
	app, directory, operations, manifest, _ := offlineFixture(t)
	// Use the full exporter schema, including distribution-only archive metadata.
	if err := json.Unmarshal([]byte(`{"file":"images.tar","size":10,"sha256":"archive-hash","parts":[{"file":"images.tar.part001","size":10,"sha256":"part-hash"}]}`), &manifest.Archive); err != nil {
		t.Fatal(err)
	}
	manifest.Platform = "linux/amd64"
	manifest.SourceCommit = strings.Repeat("a", 40)
	writeOfflineMarker(t, directory, manifest)
	state, err := app.ManageRuntime("start")
	if err != nil || !state.Running || !state.Observed {
		t.Fatalf("start: %+v %v", state, err)
	}
	if _, err = app.ManageRuntime("stop"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"config", "--services"}, {"up", "-d", "--no-build", "--pull", "never"}, {"ps", "--all", "--format", "json"}, {"config", "--services"}, {"stop"}, {"ps", "--all", "--format", "json"}}
	if !reflect.DeepEqual(*operations, want) {
		t.Fatalf("commands: %v", *operations)
	}
}

func TestOfflineInvalidPackageNeverStartsServices(t *testing.T) {
	for _, issue := range []string{"version", "unknown-field", "hash", "extra-image", "missing-image", "invalid-id", "wrong-config-image", "build", "pull", "missing-service", "inspect", "missing-local-image", "json", "trailing", "oversize", "directory"} {
		t.Run(issue, func(t *testing.T) {
			app, directory, operations, manifest, config := offlineFixture(t)
			services := config["services"].(map[string]any)
			switch issue {
			case "version":
				manifest.Version = 2
			case "unknown-field":
				data, _ := json.Marshal(manifest)
				os.WriteFile(filepath.Join(directory, "proofcode-offline.json"), append(data[:len(data)-1], []byte(`,"unexpected":true}`)...), 0600)
				if _, err := app.ManageRuntime("start"); err == nil {
					t.Fatal("unknown manifest field accepted")
				}
				for _, args := range *operations {
					if args[0] == "up" {
						t.Fatalf("services started: %v", args)
					}
				}
				return
			case "hash":
				manifest.ComposeSHA256 = strings.Repeat("0", 64)
			case "extra-image":
				manifest.Images["other"] = manifest.Images["web"]
			case "missing-image":
				delete(manifest.Images, "web")
			case "invalid-id":
				manifest.Images["web"] = "proofcode/web:latest"
			case "wrong-config-image":
				services["web"].(map[string]any)["image"] = "proofcode/web:latest"
			case "build":
				services["web"].(map[string]any)["build"] = map[string]string{"context": "."}
			case "pull":
				services["web"].(map[string]any)["pull_policy"] = "always"
			case "missing-service":
				delete(services, "web")
			case "inspect", "missing-local-image":
				original := app.runtimeDockerRunner
				app.runtimeDockerRunner = func(ctx context.Context, args ...string) (string, error) {
					if args[0] == "image" {
						if issue == "inspect" {
							return "sha256:wrong", nil
						}
						return "", errors.New("missing")
					}
					return original(ctx, args...)
				}
			}
			writeOfflineMarker(t, directory, manifest)
			marker := filepath.Join(directory, "proofcode-offline.json")
			switch issue {
			case "json":
				os.WriteFile(marker, []byte("{"), 0600)
			case "trailing":
				file, _ := os.OpenFile(marker, os.O_APPEND|os.O_WRONLY, 0600)
				file.WriteString("{}")
				file.Close()
			case "oversize":
				os.WriteFile(marker, []byte(strings.Repeat(" ", 16385)), 0600)
			case "directory":
				os.Remove(marker)
				os.Mkdir(marker, 0700)
			}
			if _, err := app.ManageRuntime("start"); err == nil {
				t.Fatal("invalid offline deployment accepted")
			}
			for _, args := range *operations {
				if args[0] == "up" {
					t.Fatalf("services started: %v", args)
				}
			}
		})
	}
}

func TestOfflineExportedPackageWithRealDocker(t *testing.T) {
	directory := os.Getenv("PROOFCODE_OFFLINE_TEST_DIRECTORY")
	if directory == "" {
		t.Skip("explicit offline package directory required")
	}
	app := &App{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	offline, err := app.offlineRuntime(ctx, directory)
	if err != nil || !offline {
		t.Fatalf("exported package: %v %v", offline, err)
	}
}
