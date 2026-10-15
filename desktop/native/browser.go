package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	cdruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BrowserState struct {
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Image   string   `json:"image"`
	Text    string   `json:"text"`
	Console []string `json:"console"`
	Width   int      `json:"width"`
	Height  int      `json:"height"`
}
type browserRun struct {
	handle        string
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	console       []string
	profile       string
	inspector     context.Context
	stopInspector context.CancelFunc
}

func browserExecutable() string {
	if value := os.Getenv("PROOFCODE_BROWSER_EXECUTABLE"); value != "" {
		return value
	}
	for _, base := range []string{os.Getenv("PROGRAMFILES(X86)"), os.Getenv("PROGRAMFILES"), os.Getenv("LOCALAPPDATA")} {
		for _, part := range []string{"Microsoft/Edge/Application/msedge.exe", "Google/Chrome/Application/chrome.exe"} {
			path := filepath.Join(base, filepath.FromSlash(part))
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}
func validBrowserURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && len(value) <= 4096
}

// Temporary project profile; never attaches to the personal browser's logins.
func (a *App) ProjectBrowser(handle, action, value string, x, y int) (BrowserState, error) {
	a.projectMu.Lock()
	_, err := a.projectByHandle(handle)
	a.projectMu.Unlock()
	if err != nil {
		return BrowserState{}, err
	}
	a.browserMu.Lock()
	defer a.browserMu.Unlock()
	if action == "close" {
		if a.browser != nil && a.browser.handle == handle {
			a.browser.cancel()
			a.browser = nil
		}
		return BrowserState{Console: []string{}}, nil
	}
	if a.browser == nil || a.browser.handle != handle {
		if action != "open" {
			return BrowserState{}, errors.New("open a URL for this project first")
		}
		if !validBrowserURL(value) {
			return BrowserState{}, errors.New("HTTP or HTTPS URL without embedded credentials required")
		}
		if a.browser != nil {
			a.browser.cancel()
			a.browser = nil
		}
		profile, profileErr := os.MkdirTemp("", "proofcode-browser-")
		if profileErr != nil {
			return BrowserState{}, profileErr
		}
		options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserDataDir(profile), chromedp.Flag("remote-debugging-address", "127.0.0.1"), chromedp.Flag("remote-allow-origins", "devtools://devtools"), chromedp.WindowSize(1280, 800), chromedp.Flag("disable-background-networking", true))
		if executable := browserExecutable(); executable != "" {
			options = append(options, chromedp.ExecPath(executable))
		}
		alloc, stopAllocator := chromedp.NewExecAllocator(context.Background(), options...)
		ctx, stopBrowser := chromedp.NewContext(alloc)
		run := &browserRun{handle: handle, ctx: ctx, profile: profile, console: []string{}}
		run.cancel = func() {
			if run.stopInspector != nil {
				run.stopInspector()
			}
			stopBrowser()
			stopAllocator()
			_ = os.RemoveAll(profile)
		}
		chromedp.ListenTarget(ctx, func(event any) {
			if called, ok := event.(*cdruntime.EventConsoleAPICalled); ok {
				parts := []string{}
				for _, arg := range called.Args {
					if len(arg.Value) > 0 {
						parts = append(parts, string(arg.Value))
					} else {
						parts = append(parts, arg.Description)
					}
				}
				text := string(called.Type) + ": " + strings.Join(parts, " ")
				if len(text) > 4000 {
					text = text[:4000]
				}
				run.mu.Lock()
				run.console = append(run.console, text)
				if len(run.console) > 100 {
					run.console = run.console[len(run.console)-100:]
				}
				run.mu.Unlock()
			}
		})
		a.browser = run
		// Allocate the browser on its lifetime context. A timeout child used for
		// the first Run would close the browser when the first request returns.
		startupDeadline := time.AfterFunc(20*time.Second, run.cancel)
		err = chromedp.Run(run.ctx)
		startupDeadline.Stop()
		if err != nil {
			run.cancel()
			a.browser = nil
			return BrowserState{}, err
		}
	}
	run := a.browser
	ctx, cancel := context.WithTimeout(run.ctx, 30*time.Second)
	defer cancel()
	var actions []chromedp.Action
	switch action {
	case "open":
		if !validBrowserURL(value) {
			return BrowserState{}, errors.New("invalid browser URL")
		}
		actions = []chromedp.Action{emulation.SetDeviceMetricsOverride(1280, 800, 1, false), chromedp.Navigate(value)}
	case "refresh":
		actions = []chromedp.Action{chromedp.Reload()}
	case "back":
		actions = []chromedp.Action{chromedp.Evaluate("history.back()", nil)}
	case "forward":
		actions = []chromedp.Action{chromedp.Evaluate("history.forward()", nil)}
	case "click":
		if x < 0 || x >= 1280 || y < 0 || y >= 800 {
			return BrowserState{}, errors.New("click outside viewport")
		}
		actions = []chromedp.Action{chromedp.MouseClickXY(float64(x), float64(y))}
	case "type":
		if len(value) > 16000 {
			return BrowserState{}, errors.New("input too long")
		}
		actions = []chromedp.Action{input.InsertText(value)}
	case "enter":
		actions = []chromedp.Action{chromedp.KeyEvent("\r")}
	case "scroll":
		if y < -1600 || y > 1600 {
			return BrowserState{}, errors.New("invalid scroll delta")
		}
		actions = []chromedp.Action{chromedp.Evaluate("window.scrollBy(0,"+strconv.Itoa(y)+")", nil)}
	case "inspect":
	case "devtools":
		if err = a.openDevTools(run); err != nil {
			return BrowserState{}, err
		}
	default:
		return BrowserState{}, errors.New("unsupported browser action")
	}
	if err = chromedp.Run(ctx, actions...); err != nil {
		return BrowserState{}, err
	}
	state := BrowserState{Width: 1280, Height: 800}
	var image []byte
	if err = chromedp.Run(ctx, chromedp.Location(&state.URL), chromedp.Title(&state.Title), chromedp.Evaluate("(document.body?.innerText || '').slice(0,64000)", &state.Text), chromedp.CaptureScreenshot(&image)); err != nil {
		return state, err
	}
	if len(image) > 8<<20 {
		return state, errors.New("browser screenshot exceeds size limit")
	}
	state.Image = "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
	run.mu.Lock()
	state.Console = append([]string{}, run.console...)
	run.mu.Unlock()
	return state, nil
}
func (a *App) shutdown(ctx context.Context) {
	a.browserMu.Lock()
	if a.browser != nil {
		a.browser.cancel()
		a.browser = nil
	}
	a.browserMu.Unlock()
	a.projectMu.Lock()
	defer a.projectMu.Unlock()
	for _, run := range a.merges {
		_ = os.RemoveAll(run.dir)
	}
	a.protocolMu.Lock()
	for _, run := range a.protocols {
		run.mu.Lock()
		running := run.state.Running
		run.mu.Unlock()
		if running {
			_ = stopCommandTree(run.cmd)
			run.cancel()
		}
	}
	a.protocolMu.Unlock()
	for _, run := range a.commands {
		run.mu.Lock()
		running := run.state.Running
		run.mu.Unlock()
		if running {
			_ = stopCommandTree(run.cmd)
			run.cancel()
		}
	}
}

// Use the browser's bundled DevTools against the exact existing project target.
// The inspector has its own disposable profile and never uses personal logins.
func (a *App) openDevTools(run *browserRun) error {
	if run.inspector != nil {
		return nil
	}
	active, err := os.ReadFile(filepath.Join(run.profile, "DevToolsActivePort"))
	if err != nil {
		return err
	}
	port := strings.Split(strings.TrimSpace(string(active)), "\n")[0]
	if _, err = strconv.Atoi(port); err != nil {
		return err
	}
	target := chromedp.FromContext(run.ctx).Target.TargetID
	inspectorURL := fmt.Sprintf("devtools://devtools/bundled/inspector.html?ws=127.0.0.1:%s/devtools/page/%s", port, target)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", os.Getenv("PROOFCODE_DEVTOOLS_HEADLESS") == "1"), chromedp.WindowSize(1400, 900))
	if executable := browserExecutable(); executable != "" {
		options = append(options, chromedp.ExecPath(executable))
	}
	alloc, stopAlloc := chromedp.NewExecAllocator(context.Background(), options...)
	ctx, stopBrowser := chromedp.NewContext(alloc)
	stop := func() { stopBrowser(); stopAlloc() }
	deadline := time.AfterFunc(20*time.Second, stop)
	err = chromedp.Run(ctx, chromedp.Navigate(inspectorURL))
	deadline.Stop()
	if err != nil {
		stop()
		return fmt.Errorf("open Chromium DevTools: %w", err)
	}
	run.inspector = ctx
	run.stopInspector = stop
	return nil
}
