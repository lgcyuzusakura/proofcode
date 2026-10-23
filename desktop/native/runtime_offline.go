package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type offlineRuntimeManifest struct {
	Version       int               `json:"version"`
	ComposeSHA256 string            `json:"composeSha256"`
	Images        map[string]string `json:"images"`
	Platform      string            `json:"platform"`
	SourceCommit  string            `json:"sourceCommit"`
	Archive       struct {
		File   string `json:"file"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
		Parts  []struct {
			File   string `json:"file"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"parts"`
	} `json:"archive"`
}

var offlineImageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// A present marker always fails closed. Missing or edited images cannot trigger
// an implicit download or a source build in an offline deployment.
func (a *App) offlineRuntime(ctx context.Context, directory string) (bool, error) {
	marker := filepath.Join(directory, "proofcode-offline.json")
	info, err := os.Lstat(marker)
	if os.IsNotExist(err) {
		return false, nil
	}
	invalid := errors.New("离线运行包校验失败，请重新导入完整的离线服务包")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
		return true, invalid
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return true, invalid
	}
	var manifest offlineRuntimeManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF || manifest.Version != 1 || len(manifest.Images) != len(proofCodeComposeServices) {
		return true, invalid
	}
	hash, err := composeFileHash(directory)
	if err != nil || hash != manifest.ComposeSHA256 {
		return true, invalid
	}
	output, err := a.runCompose(ctx, directory, "config", "--format", "json")
	if err != nil {
		return true, invalid
	}
	var config struct {
		Services map[string]struct {
			Image      string `json:"image"`
			Build      any    `json:"build"`
			PullPolicy string `json:"pull_policy"`
		} `json:"services"`
	}
	if json.Unmarshal([]byte(output), &config) != nil || len(config.Services) != len(proofCodeComposeServices) {
		return true, invalid
	}
	for _, name := range proofCodeComposeServices {
		id := manifest.Images[name]
		service, exists := config.Services[name]
		if !exists || !offlineImageID.MatchString(id) || service.Image != id || service.Build != nil || service.PullPolicy != "never" {
			return true, invalid
		}
		actual, inspectErr := a.runRuntimeDocker(ctx, "image", "inspect", "--format", "{{.Id}}", id)
		if inspectErr != nil || strings.TrimSpace(actual) != id {
			return true, errors.New("离线服务镜像缺失或不匹配，请先重新导入镜像包")
		}
	}
	return true, nil
}
