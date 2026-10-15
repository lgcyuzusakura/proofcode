package browser

import (
	bridge "github.com/proofcode-dev/proofcode/agent-engine/pkg/browserbridge"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompactBrowserStateBoundsConsoleStormAndUTF8(t *testing.T) {
	s := bridge.State{Text: strings.Repeat("页面", 4000), Console: []string{"old evidence"}}
	for i := 0; i < 100; i++ {
		s.Console = append(s.Console, strings.Repeat("错误", 1000))
	}
	for i := 0; i < 80; i++ {
		s.Elements = append(s.Elements, bridge.Element{Selector: "#stable", Label: strings.Repeat("按钮", 100)})
	}
	compactBrowserState(&s)
	if len(s.Text) > 8000 || !utf8.ValidString(s.Text) || len(s.Elements) != 40 {
		t.Fatal("invalid page bound")
	}
	total := 0
	for _, line := range s.Console {
		total += len(line)
		if !utf8.ValidString(line) {
			t.Fatal("invalid console utf8")
		}
	}
	if total > 4000 || len(s.Console) > 10 {
		t.Fatal("console storm not bounded")
	}
	for _, e := range s.Elements {
		if e.Selector != "#stable" || len(e.Label) > 160 || !utf8.ValidString(e.Label) {
			t.Fatal("invalid element preview")
		}
	}
}
