package agent

import "testing"

func TestNeedsScouts(t *testing.T) {
	if needsScouts("rename one variable") {
		t.Fatal("simple task should not spawn scouts")
	}
	if !needsScouts("重构前端和后端的鉴权模块") {
		t.Fatal("cross-module task should spawn scouts")
	}
}
