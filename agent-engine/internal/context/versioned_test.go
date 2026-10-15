package context

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, e := OpenStore(filepath.Join(t.TempDir(), "private"))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func privateScope() Scope {
	return Scope{ProjectID: "p", WorkspaceID: "w", ConversationID: "c", TaskID: "t", AttemptID: "1"}
}

func TestDurableHistoryExplicitVersionRoutesAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	v1 := "package p\r\n// First version\r\nfunc Target() int { return 11 }\r\n"
	v2 := strings.Repeat("// dirty overlay\n", 4) + "package p\nfunc Target() int { return 22 }\n"
	writeSource(t, root, "target.go", v1)
	w := &WorkspaceRetriever{Root: root, ProjectID: "p", WorkspaceID: "w", TaskID: "a", AttemptID: "1", Store: store}
	first, e := w.Retrieve(ctx, "Target", "", 6)
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Evidence) != 1 || first.Evidence[0].ReferenceID == "" || first.Evidence[0].Parser != "go/ast" {
		t.Fatalf("missing exact syntax evidence: %+v", first)
	}
	writeSource(t, root, "target.go", v2)
	second, e := w.Retrieve(ctx, "Target", "", 6)
	if e != nil {
		t.Fatal(e)
	}
	if first.SnapshotID == second.SnapshotID {
		t.Fatal("dirty bytes did not change generation")
	}
	store2, e := OpenStore(store.root)
	if e != nil {
		t.Fatal(e)
	}
	restarted := &WorkspaceRetriever{Root: root, ProjectID: "p", WorkspaceID: "w", TaskID: "b", AttemptID: "2", ConversationID: "new", Store: store2}
	current, e := restarted.Retrieve(ctx, "Target", "", 6)
	if e != nil {
		t.Fatal(e)
	}
	if current.SnapshotID != second.SnapshotID || current.FilesParsed != 0 || current.FilesReused != 1 || strings.Contains(current.Content, "return 11") {
		t.Fatalf("restart failed immutable incremental reuse: %+v", current)
	}
	history, e := restarted.RetrieveVersion(ctx, "Target", "", 6, VersionSelector{Mode: "history"})
	if e != nil {
		t.Fatal(e)
	}
	if len(history.Manifests) != 2 || history.Content != "" || len(history.Evidence) != 0 {
		t.Fatalf("history must list choices instead of mixing versions: %+v", history)
	}
	old, e := restarted.RetrieveVersion(ctx, "Target", "", 6, VersionSelector{Mode: "snapshot", SnapshotID: first.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(old.Content, "return 11") || strings.Contains(old.Content, "return 22") || old.SnapshotID != first.SnapshotID {
		t.Fatalf("wrong selected version: %+v", old)
	}
	read, e := store2.Read(ctx, privateScope(), first.Evidence[0].ReferenceID, 0, MaxReadBytes)
	if e != nil || read.Content != v1 {
		t.Fatalf("source reference failed across task scopes: %+v %v", read, e)
	}
	foreign := *restarted
	foreign.ProjectID = "other"
	if _, e := foreign.RetrieveVersion(ctx, "Target", "", 6, VersionSelector{Mode: "snapshot", SnapshotID: first.SnapshotID}); e == nil {
		t.Fatal("foreign project could load manifest")
	}
	if _, e := store2.Read(ctx, Scope{ProjectID: "other", WorkspaceID: "w"}, first.Evidence[0].ReferenceID, 0, 100); e == nil {
		t.Fatal("foreign project could read source")
	}
	ref := Reference{Scope: privateScope().SourceScope(), Kind: "source", Path: "target.go", ObjectHash: digest(v1)}
	if e := restarted.ValidateCurrent(ctx, ref); e == nil {
		t.Fatal("stale source accepted for current patch")
	}
	ref.ObjectHash = digest(v2)
	if e := restarted.ValidateCurrent(ctx, ref); e != nil {
		t.Fatal(e)
	}
}

func TestContextReadOriginalOffsetsBoundsAndTamper(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	scope := privateScope()
	body := []byte(`{"content":"raw tool output","isError":false}`)
	ref, e := store.SaveReference(Reference{Scope: scope, Kind: "tool", StartByte: 0, EndByte: len(body)}, body)
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := OpenStore(store.root)
	if e != nil {
		t.Fatal(e)
	}
	part, e := restarted.Read(ctx, scope, ref.ID, 5, 7)
	if e != nil || part.Content != string(body[5:12]) || part.NextOffset != 12 || part.EOF {
		t.Fatalf("wrong byte slice: %+v %v", part, e)
	}
	for _, bounds := range [][2]int{{-1, 10}, {len(body) + 1, 10}, {0, 0}, {0, MaxReadBytes + 1}} {
		if _, e := restarted.Read(ctx, scope, ref.ID, bounds[0], bounds[1]); e == nil {
			t.Fatalf("invalid bounds accepted: %v", bounds)
		}
	}
	eof, e := restarted.Read(ctx, scope, ref.ID, len(body), 1)
	if e != nil || !eof.EOF || eof.Content != "" {
		t.Fatal("EOF read failed")
	}
	for _, foreign := range []Scope{{ProjectID: "p", WorkspaceID: "w", ConversationID: "other", TaskID: "t", AttemptID: "1"}, {ProjectID: "p", WorkspaceID: "w", ConversationID: "c", TaskID: "t", AttemptID: "2"}} {
		if _, e := restarted.Read(ctx, foreign, ref.ID, 0, 10); e == nil {
			t.Fatal("private raw log leaked to foreign context")
		}
	}
	if _, e := restarted.Read(ctx, scope, "../../objects", 0, 10); e == nil {
		t.Fatal("reference traversal accepted")
	}
	if e := os.WriteFile(filepath.Join(store.root, "objects", ref.ObjectHash), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := restarted.Read(ctx, scope, ref.ID, 0, 10); e == nil {
		t.Fatal("corrupt raw object was accepted")
	}
}

func TestContextReadPartialUTF8PreservesRawBytesWithoutReplacement(t *testing.T) {
	store := openTestStore(t)
	scope := privateScope()
	body := []byte("中文原文")
	ref, e := store.SaveReference(Reference{Scope: scope, Kind: "tool", EndByte: len(body)}, body)
	if e != nil {
		t.Fatal(e)
	}
	part, e := store.Read(context.Background(), scope, ref.ID, 1, 1)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := base64.StdEncoding.DecodeString(part.BytesBase64)
	if e != nil || string(raw) != string(body[1:2]) || part.ValidUTF8 || part.Content != "" {
		t.Fatalf("invalid UTF8 boundary fabricated text: %+v %v", part, e)
	}
	toolResult := (ReadTool{Store: store, Scope: scope}).Execute(context.Background(), json.RawMessage(`{"referenceId":"`+ref.ID+`","offset":1,"maxBytes":1}`))
	if toolResult.IsError || toolResult.Metadata["readBytes"] != 1 {
		t.Fatalf("raw byte read was not counted at a UTF8 boundary: %+v", toolResult)
	}
	full, e := store.Read(context.Background(), scope, ref.ID, 0, len(body))
	if e != nil || !full.ValidUTF8 || full.Content != string(body) {
		t.Fatal("complete Unicode raw read lost bytes")
	}
}

func TestPersistentPostingsBoundToGenerationAndCorruptionFails(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t)
	writeSource(t, root, "math.go", "package p\nfunc Add(a,b int) int{return a+b}\n")
	writeSource(t, root, "consumer.go", "package p\nfunc Invoice() int{return Add(1,2)}\n")
	w := &WorkspaceRetriever{Root: root, ProjectID: "p", WorkspaceID: "w", Store: store}
	r, e := w.Retrieve(context.Background(), "Add", "", 8)
	if e != nil {
		t.Fatal(e)
	}
	m, e := store.LoadManifest(privateScope().SourceScope(), r.SnapshotID)
	if e != nil || m.IndexHash == "" {
		t.Fatalf("missing durable postings: %+v %v", m, e)
	}
	index, e := w.generationIndex(m)
	if e != nil || index.ChunkCount != 2 || len(index.SymbolExact["add"]) != 1 || len(index.References["add"]) != 1 {
		t.Fatalf("syntax symbol/reference postings incorrect: %+v %v", index, e)
	}
	if e := os.WriteFile(filepath.Join(store.root, "indexes", m.IndexHash), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Retrieve(context.Background(), "Add", "", 8); e == nil {
		t.Fatal("cache reused corrupt postings generation")
	}
}

func TestASTChunksCoverOriginalBytesAndLabelFallback(t *testing.T) {
	body := "package p\r\nimport \"fmt\"\r\n\r\n// A documentation\r\nfunc A(){ fmt.Println(B()) }\r\nfunc B() int { return 1 }\r\n"
	chunks := syntaxChunks("a.go", body, 80)
	combined := ""
	for _, c := range chunks {
		combined += c.Content
		if c.Content != body[c.StartByte:c.EndByte] || c.Hash != digest(c.Content) || c.Metadata["parser"] != "go/ast" {
			t.Fatalf("nonexact AST chunk: %+v", c)
		}
	}
	if combined != body || len(chunks) < 2 || !strings.Contains(chunks[1].Symbol, "A") || !strings.Contains(chunks[1].Metadata["calls"], "B") {
		t.Fatalf("AST did not preserve bounds/calls: %+v", chunks)
	}
	for _, input := range []struct{ path, text string }{{"broken.go", "package p\nfunc Broken("}, {"a.ts", "function A() { return B(); }"}} {
		for _, c := range syntaxChunks(input.path, input.text, 80) {
			if c.Metadata["parser"] != "line-regex" {
				t.Fatal("fallback misrepresented as AST")
			}
		}
	}
}

func TestDurableCompressionPreservesProtocolConstraintsAndOriginals(t *testing.T) {
	store := openTestStore(t)
	scope := privateScope()
	log := commandLog(t, strings.Repeat("INFO deterministic verification completed\n", 70), false)
	failure := commandLog(t, "ERROR regression unresolved", true)
	call := func(id string) model.Message {
		return model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: "run_command", Arguments: json.RawMessage(`{"program":"go","args":["test"]}`)}}}
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: "always preserve constraints"}, {Role: model.RoleUser, Content: "early requirement still active"}, call("a"), {Role: model.RoleTool, ToolCallID: "a", Content: log}, {Role: model.RoleUser, Content: "latest goal"}, call("b"), {Role: model.RoleTool, ToolCallID: "b", Content: log}, call("err"), {Role: model.RoleTool, ToolCallID: "err", Content: failure}}
	before, _ := json.Marshal(messages)
	view, report, e := CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 20000, OutputReserve: 1000})
	if e != nil {
		t.Fatal(e)
	}
	after, _ := json.Marshal(messages)
	if string(before) != string(after) {
		t.Fatal("durable history mutated")
	}
	if report.DeduplicatedMessages != 1 || report.ViewHash == "" || report.LedgerHash == "" || report.Lossless || report.TranscriptReferenceID == "" {
		t.Fatalf("invalid durable report: %+v", report)
	}
	if _, e := protocolGroups(view); e != nil {
		t.Fatal(e)
	}
	if view[1].Content != messages[1].Content || view[4].Content != messages[4].Content || view[8].Content != failure || view[6].Content != log || view[3].Content == log {
		t.Fatal("wrong protected or compressed messages")
	}
	restarted, e := OpenStore(store.root)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := restarted.Read(context.Background(), scope, report.SourceReferences[0], 0, MaxReadBytes)
	if e != nil || raw.Content != log {
		t.Fatalf("original tool JSON lost: %+v %v", raw, e)
	}
	transcript, e := restarted.Read(context.Background(), scope, report.TranscriptReferenceID, 0, MaxReadBytes)
	if e != nil || transcript.Content != string(before) {
		t.Fatal("original transcript not retrievable after restart")
	}
	if _, e := store.read("views", report.ViewHash, 1<<20); e != nil {
		t.Fatal(e)
	}
	if _, e := store.read("ledgers", report.LedgerHash, 1<<20); e != nil {
		t.Fatal(e)
	}
	_, failed, e := CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 50})
	if e == nil || failed.Error == "" || failed.ViewHash == "" {
		t.Fatal("protected budget violation must fail and persist failure view")
	}
}

func TestBudgetNeverDropsAnyUsersErrorsApprovalOrToolHalf(t *testing.T) {
	call := func(id string) model.Message {
		return model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: id, Name: "run_command", Arguments: json.RawMessage(`{}`)}}}
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: "system"}, {Role: model.RoleUser, Content: "early user constraint"}, call("success"), {Role: model.RoleTool, ToolCallID: "success", Content: commandLog(t, strings.Repeat("INFO old log\n", 100), false)}, call("failure"), {Role: model.RoleTool, ToolCallID: "failure", Content: commandLog(t, "ERROR important", true)}, {Role: model.RoleAssistant, Content: "User approved the schema plan"}, {Role: model.RoleUser, Content: "latest goal"}, {Role: model.RoleAssistant, Content: "latest"}}
	view, r, e := SelectMessages(messages, nil, InputBudget{ContextTokens: 250})
	if e != nil {
		t.Fatal(e)
	}
	if r.DroppedMessages != 2 || len(view) != len(messages)-2 {
		t.Fatalf("wrong pruned groups: %+v", r)
	}
	if _, e := protocolGroups(view); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(view)
	for _, value := range []string{"early user constraint", "latest goal", "ERROR important", "User approved"} {
		if !strings.Contains(string(encoded), value) {
			t.Fatalf("protected fact dropped: %s", value)
		}
	}
	if _, _, e := SelectMessages(messages, nil, InputBudget{ContextTokens: 10}); e == nil {
		t.Fatal("protected over-budget did not fail")
	}
}

func TestBudgetOmissionLeavesDurableTranscriptNavigationInView(t *testing.T) {
	store := openTestStore(t)
	scope := privateScope()
	call := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "old", Name: "read_file", Arguments: json.RawMessage(`{"path":"old.go"}`)}}}
	body, _ := json.Marshal(map[string]any{"isError": false, "content": strings.Repeat("original important source line\n", 150), "metadata": map[string]any{"path": "old.go"}})
	messages := []model.Message{{Role: model.RoleSystem, Content: "system"}, {Role: model.RoleUser, Content: "preserve user"}, call, {Role: model.RoleTool, ToolCallID: "old", Content: string(body)}, {Role: model.RoleAssistant, Content: "latest"}}
	view, report, e := CompressDurable(context.Background(), store, scope, messages, nil, InputBudget{ContextTokens: 400})
	if e != nil {
		t.Fatal(e)
	}
	if report.Budget.DroppedMessages != 2 || len(view) != 4 || !strings.Contains(view[0].Content, report.TranscriptReferenceID) {
		t.Fatalf("omission lacks recoverable navigation: %+v %+v", view, report)
	}
	if report.Changes[0].MessageIndex != 2 || report.Changes[1].MessageIndex != 3 {
		t.Fatal("navigation shifted original omission coordinates")
	}
	raw, e := store.Read(context.Background(), scope, report.TranscriptReferenceID, 0, MaxReadBytes)
	if e != nil || !strings.Contains(raw.Content, "original important source line") {
		t.Fatal("omitted source inaccessible")
	}
}

func TestContextToolsRejectUnknownScopeArgumentsAndNoMixedHistory(t *testing.T) {
	store := openTestStore(t)
	scope := privateScope()
	ref, e := store.SaveReference(Reference{Scope: scope, Kind: "tool", EndByte: 3}, []byte("raw"))
	if e != nil {
		t.Fatal(e)
	}
	r := ReadTool{Store: store, Scope: scope}
	for _, raw := range []string{`{"referenceId":"` + ref.ID + `","offset":0,"maxBytes":10,"projectId":"foreign"}`, `{"referenceId":"` + ref.ID + `","maxBytes":10}`, `{"referenceId":"` + ref.ID + `","offset":0,"maxBytes":10} {}`} {
		if !r.Execute(context.Background(), json.RawMessage(raw)).IsError {
			t.Fatalf("invalid tool arguments accepted: %s", raw)
		}
	}
	if r.Execute(context.Background(), json.RawMessage(`{"referenceId":"`+ref.ID+`","offset":0,"maxBytes":10}`)).IsError {
		t.Fatal("valid read tool failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := store.Read(ctx, scope, ref.ID, 0, 10); e != context.Canceled {
		t.Fatal("cancelled read proceeded")
	}
}
