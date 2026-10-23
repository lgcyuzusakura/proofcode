package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func runtimeZipFixture(t *testing.T, mutate func(map[string][]byte, *runtimeBundleManifest)) []byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"compose.yml", ".dockerignore", "control-plane/Dockerfile", "control-plane/pom.xml", "agent-engine/go.mod", "agent-engine/Dockerfile", "frontend/package.json", "frontend/package-lock.json", "frontend/Dockerfile", "frontend/src/main.tsx"} {
		files[name] = []byte("fixture " + name)
	}
	manifest := runtimeBundleManifest{Version: 1, Release: "0.4.0"}
	for name, data := range files {
		hash := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, runtimeBundleFile{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])})
	}
	if mutate != nil {
		mutate(files, &manifest)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["runtime-manifest.json"] = raw
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil {
		t.Fatal("zip close failed")
	}
	return output.Bytes()
}
func TestBundledRuntimePreparesIndependentFilesWithoutDockerOrBackendChanges(t *testing.T) {
	parent := t.TempDir()
	app := &App{registryDir: t.TempDir(), runtimeBundleData: runtimeZipFixture(t, nil)}
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return parent, nil }
	app.runtimeRunner = func(context.Context, string, ...string) (string, error) {
		t.Fatal("preparation must not run Docker Compose")
		return "", nil
	}
	app.runtimeDockerRunner = func(context.Context, ...string) (string, error) {
		t.Fatal("preparation must not run Docker")
		return "", nil
	}
	config, err := app.SaveRuntimeConnection("https://backend.example.test")
	if err != nil {
		t.Fatal(err)
	}
	info, err := app.GetBundledRuntime()
	if err != nil || !info.Available || info.Files != 10 || info.Release != "0.4.0" {
		t.Fatalf("metadata: %+v %v", info, err)
	}
	state, err := app.PrepareBundledRuntime(true, false)
	if err != nil || !state.Configured || state.Observed || state.Running || state.Busy {
		t.Fatalf("prepare: %+v %v", state, err)
	}
	if !sameRuntimePath(state.Directory, filepath.Join(parent, "ProofCode-runtime")) {
		t.Fatal("wrong destination")
	}
	after, _ := app.GetRuntimeConnection()
	if after != config {
		t.Fatal("preparation changed backend or auth destination")
	}
	env, err := os.ReadFile(filepath.Join(state.Directory, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]bool{}
	for _, key := range []string{"DEV_AUTH_TOKEN", "RUNNER_TOKEN", "POSTGRES_PASSWORD", "ARTEMIS_PASSWORD", "DATA_SNAPSHOT_KEY"} {
		found := false
		for _, line := range strings.Split(string(env), "\n") {
			if strings.HasPrefix(line, key+"=") {
				found = true
				value := strings.TrimPrefix(line, key+"=")
				if len(value) < 40 || strings.Contains(state.Message, value) {
					t.Fatal("weak or exposed secret")
				}
				if key == "DATA_SNAPSHOT_KEY" {
					data, err := base64.StdEncoding.DecodeString(value)
					if err != nil || len(data) != 32 {
						t.Fatal("invalid encryption key")
					}
					value = hex.EncodeToString(data)
				}
				if secrets[value] {
					t.Fatal("credentials must be generated independently")
				}
				secrets[value] = true
			}
		}
		if !found {
			t.Fatalf("missing credential: %s", key)
		}
	}
	if !bytes.Contains(env, []byte("RUNNER_ALLOW_WRITE=true\nRUNNER_ALLOW_EXEC=false\n")) {
		t.Fatal("permission choices not preserved")
	}
	before := append([]byte{}, env...)
	if _, err = app.PrepareBundledRuntime(false, true); err == nil {
		t.Fatal("existing runtime overwritten")
	}
	env, _ = os.ReadFile(filepath.Join(state.Directory, ".env"))
	if !bytes.Equal(env, before) {
		t.Fatal("existing data key changed")
	}
	restored, err := (&App{registryDir: app.registryDir}).GetRuntimeManagement()
	if err != nil || !restored.Configured || restored.Directory != state.Directory {
		t.Fatalf("restore %+v %v", restored, err)
	}
}
func TestBundledRuntimeCancelUnavailableAndSingleFlightPreserveExistingSetup(t *testing.T) {
	app, original, _ := runtimeFixture(t)
	app.runtimeBundleData = runtimeZipFixture(t, nil)
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) { return "", nil }
	state, err := app.PrepareBundledRuntime(false, false)
	if err != nil || state.Directory != original || !state.Configured {
		t.Fatalf("cancel %+v %v", state, err)
	}
	app.runtimeBundleData = []byte{}
	if info, err := app.GetBundledRuntime(); err != nil || info.Available {
		t.Fatalf("unavailable %+v %v", info, err)
	}
	if _, err = app.PrepareBundledRuntime(false, false); err == nil {
		t.Fatal("empty runtime accepted")
	}
	app.runtimeBundleData = runtimeZipFixture(t, nil)
	ctx, err := app.beginRuntimeOperation("starting", 60e9)
	if err != nil {
		t.Fatal(err)
	}
	_ = ctx
	if _, err = app.PrepareBundledRuntime(false, false); err == nil {
		t.Fatal("parallel mutation allowed")
	}
	app.closeRuntimeOperations()
}
func TestBundledRuntimeRejectsCorruptUnmanifestedPrivateAndInvalidPaths(t *testing.T) {
	for _, issue := range []string{"hash", "size", "private", "unmanifested", "duplicate", "missing"} {
		t.Run(issue, func(t *testing.T) {
			data := runtimeZipFixture(t, func(files map[string][]byte, manifest *runtimeBundleManifest) {
				switch issue {
				case "hash":
					manifest.Files[0].SHA256 = strings.Repeat("0", 64)
				case "size":
					manifest.Files[0].Size++
				case "private":
					files["thesis/paper.pdf"] = []byte("PRIVATE")
				case "unmanifested":
					files["frontend/src/unlisted.ts"] = []byte("extra")
				case "duplicate":
					manifest.Files[0] = manifest.Files[1]
				case "missing":
					delete(files, "compose.yml")
				}
			})
			if _, err := verifyRuntimeBundle(data); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("invalid bundle accepted or exposed data")
			}
		})
	}
	for _, name := range []string{"../outside.go", "frontend/src/../../escape.go", "C:/escape.go", "frontend/src/NUL.ts", "frontend/src/name.ts.", "frontend/src/.env.ts", "frontend/src/论文.ts", "frontend/src/file\\.ts", ".git/config", "frontend/src/file.pdf", "frontend/src/file.ts:stream"} {
		if runtimeSourcePath(name) {
			t.Fatalf("unsafe path accepted: %s", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bundle, err := verifyRuntimeBundle(runtimeZipFixture(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if _, err = prepareRuntimeBundle(ctx, bundle, parent, false, false); err == nil {
		t.Fatal("cancelled extraction completed")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 0 {
		t.Fatal("cancelled extraction left staging or published files")
	}
}
func TestEmbeddedRuntimeCodeOnlyAndRealComposeConfiguration(t *testing.T) {
	app := &App{registryDir: t.TempDir()}
	data, err := app.runtimeBundle()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Skip("run desktop/prepare-runtime-bundle.ps1 before bundle integration test")
	}
	bundle, err := verifyRuntimeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	for name := range bundle.files {
		if strings.HasSuffix(name, ".docx") || strings.HasSuffix(name, ".pdf") || strings.Contains(name, "thesis") || strings.Contains(name, ".git/") || strings.Contains(name, ".env") {
			t.Fatalf("private source included: %s", name)
		}
	}
	directory, err := prepareRuntimeBundle(context.Background(), bundle, t.TempDir(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(directory, "frontend/pdf-resources.ts")); err != nil {
		t.Fatal("shared PDF build helper missing")
	}
	if os.Getenv("PROOFCODE_RUNTIME_BUNDLE_COMPOSE_TEST") != "1" {
		return
	}
	// compose config parses locally; it never connects to the daemon or starts services.
	command := exec.Command("docker", "compose", "--project-directory", directory, "-f", filepath.Join(directory, "compose.yml"), "config", "--format", "json")
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("standalone runtime compose config rejected: %v", err)
	}
	var parsed struct {
		Services map[string]struct {
			Ports []struct {
				HostIP string `json:"host_ip"`
			}
			Environment map[string]string
		}
	}
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Services) != 6 {
		t.Fatal("invalid compose service configuration")
	}
	for name, service := range parsed.Services {
		for _, port := range service.Ports {
			if (name != "web" && name != "control-plane") || port.HostIP != "127.0.0.1" {
				t.Fatal("runtime exposes private backend services")
			}
		}
	}
	if parsed.Services["runner"].Environment["RUNNER_ALLOW_WRITE"] != "false" || parsed.Services["runner"].Environment["RUNNER_ALLOW_EXEC"] != "false" {
		t.Fatal("runner permission choices ignored")
	}
	if parsed.Services["artemis"].Environment["ARTEMIS_PASSWORD"] != parsed.Services["control-plane"].Environment["SPRING_ARTEMIS_PASSWORD"] {
		t.Fatal("broker and control credentials differ")
	}
}

func TestBundledRuntimeUIEndToEnd(t *testing.T) {
	if os.Getenv("PROOFCODE_RUNTIME_UI_SMOKE") != "1" {
		t.Skip("opt-in: existing UI server, Edge and Playwright required")
	}
	app := &App{registryDir: t.TempDir()}
	defer app.closeRuntimeOperations()
	info, err := app.GetBundledRuntime()
	if err != nil || !info.Available {
		t.Fatal("generate the runtime source bundle before this integration test")
	}
	parent := t.TempDir()
	var cancel atomic.Bool
	var dockerCalls atomic.Int32
	app.runtimeDirectoryPicker = func(context.Context, string) (string, error) {
		if cancel.Swap(false) {
			return "", nil
		}
		return parent, nil
	}
	app.runtimeRunner = func(context.Context, string, ...string) (string, error) {
		dockerCalls.Add(1)
		return "", errors.New("unexpected Compose mutation")
	}
	app.runtimeDockerRunner = func(context.Context, ...string) (string, error) {
		dockerCalls.Add(1)
		return "", errors.New("unexpected Docker mutation")
	}
	secret, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("PROOFCODE_UI_URL")
		if origin == "" {
			origin = "http://127.0.0.1:5177"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var result any
		var callErr error
		switch r.URL.Path {
		case "/get":
			result, callErr = app.GetRuntimeManagement()
		case "/bundle":
			result, callErr = app.GetBundledRuntime()
		case "/cancel":
			cancel.Store(true)
			result = true
		case "/prepare":
			var args struct{ Write, Execute bool }
			if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&args) != nil {
				http.Error(w, "invalid parameters", http.StatusBadRequest)
				return
			}
			result, callErr = app.PrepareBundledRuntime(args.Write, args.Execute)
		default:
			http.NotFound(w, r)
			return
		}
		if callErr != nil {
			http.Error(w, "native operation failed", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	config, _ := json.Marshal(map[string]string{"url": server.URL, "token": secret})
	script, err := filepath.Abs(filepath.Join("..", "..", "frontend", "tests", "runtime-setup-native.browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "PROOFCODE_RUNTIME_SETUP_NATIVE="+string(config))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime bundle UI: %v\n%s", err, output)
	}
	state, err := app.GetRuntimeManagement()
	if err != nil || !state.Configured || state.Running || dockerCalls.Load() != 0 {
		t.Fatal("preparation unexpectedly mutated services")
	}
	env, err := os.ReadFile(filepath.Join(state.Directory, ".env"))
	if err != nil || !bytes.Contains(env, []byte("RUNNER_ALLOW_WRITE=false\nRUNNER_ALLOW_EXEC=true\n")) {
		t.Fatal("browser permission choices did not reach the real native configuration")
	}
	if _, err = os.Stat(filepath.Join(state.Directory, "frontend/src/PreviewWorkspace.tsx")); err != nil {
		t.Fatal("preview source not available in independently prepared runtime")
	}
	t.Log(string(output))
}
