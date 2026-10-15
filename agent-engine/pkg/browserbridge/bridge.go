// Package browserbridge connects a local runner to the desktop-owned project tab.
// The descriptor and bearer capability live outside the repository.
package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ProjectID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

type Descriptor struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}
type Action struct {
	Action   string `json:"action"`
	Value    string `json:"value,omitempty"`
	Selector string `json:"selector,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
}
type Element struct {
	Selector string `json:"selector"`
	Role     string `json:"role"`
	Label    string `json:"label"`
}
type State struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Image    string    `json:"image"`
	Text     string    `json:"text"`
	Console  []string  `json:"console"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
	Owner    string    `json:"owner"`
	TargetID string    `json:"targetId"`
	Elements []Element `json:"elements"`
	Revision uint64    `json:"revision"`
}

func DescriptorPath() (string, error) {
	if p := os.Getenv("PROOFCODE_BROWSER_BRIDGE_FILE"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ProofCode", "browser-bridge.json"), nil
}
func Load() (Descriptor, error) {
	p, err := DescriptorPath()
	if err != nil {
		return Descriptor{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Descriptor{}, errors.New("共享浏览器未启动，请在桌面端打开项目浏览器")
	}
	var d Descriptor
	if len(b) > 4096 || json.Unmarshal(b, &d) != nil || !validDescriptor(d) {
		return Descriptor{}, errors.New("共享浏览器本机描述文件无效")
	}
	return d, nil
}
func validDescriptor(d Descriptor) bool {
	u, err := url.Parse(d.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback() && u.Port() != "" && len(d.Token) == 64 && !strings.ContainsAny(d.Token, "\r\n")
}
func (d Descriptor) Call(ctx context.Context, project string, action Action) (State, error) {
	if !validDescriptor(d) || !ProjectID.MatchString(project) {
		return State{}, errors.New("共享浏览器作用域无效")
	}
	b, err := json.Marshal(action)
	if err != nil {
		return State{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", d.URL+"/projects/"+project+"/browser", bytes.NewReader(b))
	if err != nil {
		return State{}, errors.New("共享浏览器请求无效")
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return State{}, errors.New("无法连接桌面共享浏览器")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (12<<20)+1))
	if err != nil || len(data) > 12<<20 {
		return State{}, errors.New("共享浏览器响应超过限制")
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 409 {
			return State{}, errors.New("浏览器正在人工接管；请等待用户交还 AI 控制")
		}
		return State{}, errors.New("共享浏览器拒绝操作，请检查项目绑定和浏览器状态")
	}
	var state State
	if json.Unmarshal(data, &state) != nil {
		return State{}, errors.New("共享浏览器状态格式无效")
	}
	return state, nil
}
