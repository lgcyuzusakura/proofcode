package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed runtime-assets/*
var bundledRuntimeAssets embed.FS

type BundledRuntime struct {
	Available bool   `json:"available"`
	Release   string `json:"release"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}
type runtimeBundleFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type runtimeBundleManifest struct {
	Version int                 `json:"version"`
	Release string              `json:"release"`
	Files   []runtimeBundleFile `json:"files"`
}
type verifiedRuntimeBundle struct {
	info     BundledRuntime
	files    map[string]*zip.File
	manifest runtimeBundleManifest
}

func (a *App) runtimeBundle() ([]byte, error) {
	if a.runtimeBundleData != nil {
		return a.runtimeBundleData, nil
	}
	data, err := bundledRuntimeAssets.ReadFile("runtime-assets/runtime.zip")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("内置运行包无法读取")
	}
	return data, nil
}

// Match the packager's code-only roots. Neither Git history nor arbitrary
// documents can become executable deployment inputs, even in a malformed zip.
func runtimeSourcePath(name string) bool {
	if len(name) > 240 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00<>\"|?*\r\n\t") || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.Contains(lower, "thesis") || strings.Contains(part, "论文") || strings.Contains(part, "論文") {
			return false
		}
		if lower == ".git" || lower == ".proofcode" || strings.HasPrefix(lower, ".env") || regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\.|$)`).MatchString(part) {
			return false
		}
	}
	exact := []string{"compose.yml", ".dockerignore", "agent-engine/Dockerfile", "agent-engine/.dockerignore", "agent-engine/go.mod", "agent-engine/go.sum", "agent-engine/runner-entrypoint.sh", "control-plane/Dockerfile", "control-plane/pom.xml", "control-plane/settings.xml", "frontend/Dockerfile", "frontend/.dockerignore", "frontend/package.json", "frontend/package-lock.json", "frontend/index.html", "frontend/tsconfig.json", "frontend/vite.config.ts", "frontend/pdf-resources.ts", "frontend/nginx.conf"}
	for _, file := range exact {
		if name == file {
			return true
		}
	}
	for _, prefix := range []string{"agent-engine/cmd/", "agent-engine/internal/", "agent-engine/pkg/"} {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	if strings.HasPrefix(name, "control-plane/src/main/java/") && strings.HasSuffix(name, ".java") {
		return true
	}
	if strings.HasPrefix(name, "control-plane/src/main/resources/") {
		return strings.Contains("|.sql|.yml|.yaml|.properties|.xml|", "|"+path.Ext(name)+"|")
	}
	if strings.HasPrefix(name, "frontend/src/") && !strings.Contains(name, ".test.") && !strings.Contains(name, ".spec.") {
		return strings.Contains("|.ts|.tsx|.css|.svg|", "|"+path.Ext(name)+"|")
	}
	return false
}
func verifyRuntimeBundle(data []byte) (*verifiedRuntimeBundle, error) {
	return verifySourceBundle(data, runtimeSourcePath, []string{"compose.yml", ".dockerignore", "control-plane/Dockerfile", "control-plane/pom.xml", "agent-engine/go.mod", "agent-engine/Dockerfile", "frontend/package.json", "frontend/package-lock.json", "frontend/Dockerfile"})
}
func verifySourceBundle(data []byte, allowed func(string) bool, requiredFiles []string) (*verifiedRuntimeBundle, error) {
	invalid := errors.New("内置运行包不完整或校验失败，请使用完整的桌面发行包")
	if len(data) == 0 || len(data) > 32<<20 {
		return nil, invalid
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) > 8193 {
		return nil, invalid
	}
	files := map[string]*zip.File{}
	names := map[string]bool{}
	for _, file := range archive.File {
		lower := strings.ToLower(file.Name)
		if names[lower] || !file.Mode().IsRegular() || file.UncompressedSize64 > 8<<20 || (file.Name != "runtime-manifest.json" && !allowed(file.Name)) {
			return nil, invalid
		}
		names[lower] = true
		files[file.Name] = file
	}
	manifestFile := files["runtime-manifest.json"]
	if manifestFile == nil || manifestFile.UncompressedSize64 > 1<<20 {
		return nil, invalid
	}
	stream, err := manifestFile.Open()
	if err != nil {
		return nil, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(stream, (1<<20)+1))
	stream.Close()
	if err != nil || len(raw) > 1<<20 {
		return nil, invalid
	}
	var manifest runtimeBundleManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.Version != 1 || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(manifest.Release) || len(manifest.Files) == 0 || len(manifest.Files) != len(files)-1 {
		return nil, invalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, invalid
	}
	var size int64
	seen := map[string]bool{}
	for _, entry := range manifest.Files {
		file := files[entry.Path]
		if seen[entry.Path] || file == nil || entry.Path == "runtime-manifest.json" || entry.Size < 0 || entry.Size > 8<<20 || uint64(entry.Size) != file.UncompressedSize64 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.SHA256) {
			return nil, invalid
		}
		seen[entry.Path] = true
		size += entry.Size
		if size > 64<<20 {
			return nil, invalid
		}
		reader, err := file.Open()
		if err != nil {
			return nil, invalid
		}
		hash := sha256.New()
		n, err := io.Copy(hash, io.LimitReader(reader, entry.Size+1))
		reader.Close()
		if err != nil || n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return nil, invalid
		}
	}
	for _, required := range requiredFiles {
		if !seen[required] {
			return nil, invalid
		}
	}
	hash := sha256.Sum256(data)
	return &verifiedRuntimeBundle{info: BundledRuntime{Available: true, Release: manifest.Release, Files: len(manifest.Files), Bytes: size, SHA256: hex.EncodeToString(hash[:])}, files: files, manifest: manifest}, nil
}

// Passive: reads only the embedded build resource, never Docker or the network.
func (a *App) GetBundledRuntime() (BundledRuntime, error) {
	data, err := a.runtimeBundle()
	if err != nil {
		return BundledRuntime{}, err
	}
	if len(data) == 0 {
		return BundledRuntime{}, nil
	}
	bundle, err := verifyRuntimeBundle(data)
	if err != nil {
		return BundledRuntime{}, err
	}
	return bundle.info, nil
}
func (a *App) pickRuntimeDirectory(title string) (string, error) {
	if a.runtimeDirectoryPicker != nil {
		return a.runtimeDirectoryPicker(a.ctx, title)
	}
	if a.ctx == nil {
		return "", errors.New("本机目录选择器不可用")
	}
	value, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: title})
	if err != nil {
		return "", errors.New("无法打开目录选择器")
	}
	return value, nil
}
func runtimeSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("无法生成本机服务密钥")
	}
	return hex.EncodeToString(raw), nil
}

func writeRuntimeConfiguration(directory string, allowWrite, allowExec bool) error {
	values := make([]string, 5)
	for index := range values {
		value, err := runtimeSecret()
		if err != nil {
			return err
		}
		values[index] = value
	}
	key, err := hex.DecodeString(values[4])
	if err != nil {
		return err
	}
	text := fmt.Sprintf("# Generated locally; keep this directory private. Do not rotate the data key after use.\nDEV_AUTH_TOKEN=%s\nRUNNER_TOKEN=%s\nPOSTGRES_PASSWORD=%s\nARTEMIS_PASSWORD=%s\nDATA_SNAPSHOT_KEY=%s\nRUNNER_ALLOW_WRITE=%t\nRUNNER_ALLOW_EXEC=%t\nPROOFCODE_CONTROL_PORT=8080\nPROOFCODE_WEB_PORT=3000\n", values[0], values[1], values[2], values[3], base64.StdEncoding.EncodeToString(key), allowWrite, allowExec)
	for name, body := range map[string]string{".env": text, ".gitignore": ".env\n.env.*\n.proofcode/\n"} {
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("无法写入本机运行配置，已有文件未覆盖")
		}
		_, writeErr := io.WriteString(file, body)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("无法保存本机运行配置")
		}
	}
	return nil
}
func extractSourceBundle(ctx context.Context, bundle *verifiedRuntimeBundle, directory string) error {
	for _, entry := range bundle.manifest.Files {
		if ctx.Err() != nil {
			return errors.New("文件准备已取消")
		}
		destination := filepath.Join(directory, filepath.FromSlash(entry.Path))
		if os.MkdirAll(filepath.Dir(destination), 0700) != nil {
			return errors.New("无法准备运行文件夹")
		}
		input, err := bundle.files[entry.Path].Open()
		if err != nil {
			return errors.New("内置运行文件无法读取")
		}
		output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return errors.New("无法保存运行文件")
		}
		_, err = io.Copy(output, io.LimitReader(input, entry.Size+1))
		input.Close()
		closed := output.Close()
		if err != nil || closed != nil {
			return errors.New("运行文件写入未完成")
		}
	}
	return nil
}
func prepareRuntimeBundle(ctx context.Context, bundle *verifiedRuntimeBundle, parent string, allowWrite, allowExec bool) (string, error) {
	parent, err := filepath.Abs(parent)
	if err == nil {
		parent, err = filepath.EvalSymlinks(parent)
	}
	if err != nil {
		return "", errors.New("所选位置无法访问")
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", errors.New("请选择可写的文件夹")
	}
	target := filepath.Join(parent, "ProofCode-runtime")
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return "", errors.New("所选位置已有 ProofCode-runtime，请使用原部署目录或选择其他位置；已有文件不会覆盖")
	}
	staging, err := os.MkdirTemp(parent, ".proofcode-runtime-")
	if err != nil {
		return "", errors.New("无法在所选位置创建运行目录")
	}
	defer os.RemoveAll(staging)
	if err = extractSourceBundle(ctx, bundle, staging); err != nil {
		return "", err
	}
	if err = writeRuntimeConfiguration(staging, allowWrite, allowExec); err != nil {
		return "", err
	}
	if err = atomicJSON(filepath.Join(staging, "proofcode-runtime.json"), bundle.info); err != nil {
		return "", errors.New("无法保存运行包版本")
	}
	if ctx.Err() != nil {
		return "", errors.New("运行包准备已取消")
	}
	// Claim a new directory exclusively, then publish already verified files. On a
	// disk failure preserve any published data for recovery instead of deleting it.
	if os.Mkdir(target, 0700) != nil {
		return "", errors.New("运行目录已存在或不可写，未覆盖已有目录")
	}
	children, err := os.ReadDir(staging)
	if err != nil {
		return target, errors.New("运行目录发布未完成，请选择该目录检查文件")
	}
	for _, child := range children {
		if os.Rename(filepath.Join(staging, child.Name()), filepath.Join(target, child.Name())) != nil {
			return target, errors.New("运行目录发布未完成；已有文件已保留，请选择其他位置重试")
		}
	}
	return target, nil
}

// Preparing files neither starts services nor changes the selected backend URL.
// Model keys are configured after login and never belong in this source bundle.
func (a *App) PrepareBundledRuntime(allowWrite, allowExec bool) (state RuntimeManagement, err error) {
	ctx, err := a.beginRuntimeOperation("preparing", 2*time.Minute)
	if err != nil {
		return state, err
	}
	state, _ = a.GetRuntimeManagement()
	defer func() { state, err = a.finishRuntimeOperation(state, err) }()
	data, err := a.runtimeBundle()
	if err != nil {
		return state, err
	}
	if len(data) == 0 {
		return state, errors.New("此开发版本未内置运行包，请选择已有部署目录")
	}
	bundle, err := verifyRuntimeBundle(data)
	if err != nil {
		return state, err
	}
	parent, err := a.pickRuntimeDirectory("选择保存本机服务的位置，将新建 ProofCode-runtime 文件夹")
	if err != nil {
		return state, err
	}
	if parent == "" {
		return state, nil
	}
	directory, err := prepareRuntimeBundle(ctx, bundle, parent, allowWrite, allowExec)
	if err != nil {
		return state, err
	}
	if err = a.saveRuntimeDirectory(directory); err != nil {
		return RuntimeManagement{Directory: directory, Message: "运行文件已准备，请手动选择该部署目录。", Services: []RuntimeService{}}, errors.New("运行文件已准备，但本机部署目录保存失败，请手动选择")
	}
	return RuntimeManagement{Directory: directory, Configured: true, Message: "本机运行文件与密钥已准备。启动 Docker Desktop 后点击启动服务，首次构建需要联网。", Services: []RuntimeService{}}, nil
}
