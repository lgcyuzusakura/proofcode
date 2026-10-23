package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillReadPaginatesImmutableContentAndEvidence(t *testing.T) {
	skill := Skill{ID: "saved-id", Name: "review", Description: "Review APIs", Content: "first\nsecond\nthird", Source: "SKILL.md", Version: 7, Hash: "saved-hash"}
	reader := SkillRead{Skills: []Skill{skill}}
	first := reader.Execute(context.Background(), json.RawMessage(`{"name":"review","maxLines":2}`))
	if first.IsError || first.Metadata["skillHash"] != "saved-hash" || first.Metadata["skillVersion"] != int64(7) || first.Metadata["nextLine"] != 3 {
		t.Fatalf("missing evidence: %+v", first)
	}
	var page struct {
		Content                                  string `json:"content"`
		StartLine, EndLine, TotalLines, NextLine int
	}
	if json.Unmarshal([]byte(first.Content), &page) != nil || page.Content != "first\nsecond" || page.TotalLines != 3 || page.StartLine != 1 || page.EndLine != 2 {
		t.Fatalf("invalid first page: %s", first.Content)
	}
	last := reader.Execute(context.Background(), json.RawMessage(`{"name":"review","startLine":3}`))
	if last.IsError || json.Unmarshal([]byte(last.Content), &page) != nil || page.Content != "third" || page.NextLine != 0 {
		t.Fatalf("invalid last page: %+v", last)
	}
	info, _ := json.Marshal(skill.Info())
	if strings.Contains(string(info), "first") {
		t.Fatal("body leaked into catalog")
	}
}

func TestSkillReadRejectsUnavailableAndInvalidRequestsAndHonorsScope(t *testing.T) {
	reader := SkillRead{Skills: []Skill{{Name: "review", Content: "saved workflow"}}}
	for _, raw := range []string{`{}`, `{"name":"../SKILL.md"}`, `{"name":"review","startLine":-1}`, `{"name":"review","startLine":3}`, `{"name":"review","maxLines":201}`, `{"name":"review","path":"/secret"}`, `{"name":"review"} {}`, `[]`} {
		if !reader.Execute(context.Background(), json.RawMessage(raw)).IsError {
			t.Fatalf("accepted invalid request %s", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !reader.Execute(ctx, json.RawMessage(`{"name":"review"}`)).IsError {
		t.Fatal("cancel ignored")
	}
	scoped := Scope(NewRegistry(reader), []string{"read_file"}, true)
	if _, ok := scoped.Get("skill_read"); ok {
		t.Fatal("skill bypassed tool allowlist")
	}
	readonly := Scope(NewRegistry(reader), nil, true)
	value, _ := readonly.Get("skill_read")
	if value.Execute(context.Background(), json.RawMessage(`{"name":"review"}`)).IsError {
		t.Fatal("read-only mode rejected saved instructions")
	}
}

func TestSkillReadBoundsPagesByBytes(t *testing.T) {
	reader := SkillRead{Skills: []Skill{{Name: "review", Content: strings.Repeat(strings.Repeat("中", 100)+"\n", 100)}}}
	result := reader.Execute(context.Background(), json.RawMessage(`{"name":"review","maxLines":200}`))
	if result.IsError || result.Metadata["nextLine"].(int) <= 1 || len(result.Content) > 18000 {
		t.Fatalf("unbounded page: %+v", result.Metadata)
	}
	reader.Skills[0].Content = strings.Repeat("中", 6000)
	if !reader.Execute(context.Background(), json.RawMessage(`{"name":"review"}`)).IsError {
		t.Fatal("oversized line accepted")
	}
}
