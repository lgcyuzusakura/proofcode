package context

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type Constraint struct {
	ReferenceID  string     `json:"referenceId"`
	MessageIndex int        `json:"messageIndex"`
	Role         model.Role `json:"role"`
	SourceHash   string     `json:"sourceHash"`
	Active       bool       `json:"active"`
}

type CompressionChange struct {
	MessageIndex int    `json:"messageIndex"`
	Action       string `json:"action"`
	ReferenceID  string `json:"referenceId"`
	SourceHash   string `json:"sourceHash"`
}

// CompressDurable keeps original tool objects and the user constraint ledger
// before producing a model view. No model-generated text can supersede an
// active requirement or grant approval. Original objects do not imply lossless
// model comprehension: the report explicitly describes visible omissions.
func CompressDurable(ctx context.Context, store *Store, scope Scope, messages []model.Message, definitions []model.ToolDefinition, budget InputBudget) ([]model.Message, CompressionReport, error) {
	r := CompressionReport{Lossless: false, AlgorithmVersion: CompressionVersion, TokenCounting: "utf8-json-bytes-div-4-estimate", Changes: []CompressionChange{}}
	if store == nil {
		return nil, r, errors.New("durable compression requires a context store")
	}
	if err := scope.validate(); err != nil {
		return nil, r, err
	}
	if _, err := protocolGroups(messages); err != nil {
		return nil, r, err
	}
	full, err := json.Marshal(messages)
	if err != nil {
		return nil, r, err
	}
	store.mu.Lock()
	r.SourceHash, err = store.put("objects", full)
	store.mu.Unlock()
	if err != nil {
		return nil, r, err
	}
	transcript, e := store.SaveReference(Reference{Scope: scope, Kind: "transcript", StartByte: 0, EndByte: len(full)}, full)
	if e != nil {
		return nil, r, e
	}
	r.TranscriptReferenceID = transcript.ID
	view := cloneMessagesExact(messages)
	refs := map[int]Reference{}
	ledger := []Constraint{}
	for i, m := range messages {
		if err := ctx.Err(); err != nil {
			return nil, r, err
		}
		r.BeforeEstimatedTokens += messageTokens(m)
		if m.Role != model.RoleTool && m.Role != model.RoleUser && m.Role != model.RoleSystem {
			continue
		}
		kind := "tool"
		if m.Role != model.RoleTool {
			kind = "constraint"
		}
		ref, e := store.SaveReference(Reference{Scope: scope, Kind: kind, ToolCallID: m.ToolCallID, StartByte: 0, EndByte: len(m.Content)}, []byte(m.Content))
		if e != nil {
			return nil, r, e
		}
		refs[i] = ref
		if kind == "constraint" {
			ledger = append(ledger, Constraint{ReferenceID: ref.ID, MessageIndex: i, Role: m.Role, SourceHash: ref.SourceHash, Active: true})
		}
	}
	lb, err := json.Marshal(struct {
		Scope       Scope        `json:"scope"`
		SourceHash  string       `json:"sourceHash"`
		Constraints []Constraint `json:"constraints"`
	}{scope, r.SourceHash, ledger})
	if err != nil {
		return nil, r, err
	}
	store.mu.Lock()
	r.LedgerHash, err = store.put("ledgers", lb)
	store.mu.Unlock()
	if err != nil {
		return nil, r, err
	}
	// The most recent full copy remains. Old duplicates are JSON tool envelopes
	// that advertise navigation references, never fake original execution output.
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != model.RoleTool || !isSuccessfulOutput(m.Content) {
			continue
		}
		hash := digest(m.Content)
		if !seen[hash] {
			seen[hash] = true
			continue
		}
		ref := refs[i]
		replacement, _ := json.Marshal(map[string]any{"content": fmt.Sprintf("Duplicate successful log omitted from model view. Read the exact original JSON tool result with context_read(referenceId=%s,offset=0,maxBytes=65536). This is a navigation envelope for original tool observations.", ref.ID), "isError": false, "metadata": map[string]any{"contextReferenceId": ref.ID, "objectHash": ref.ObjectHash, "sourceHash": ref.SourceHash, "contextCompression": CompressionVersion, "originalBytes": len(m.Content)}})
		if len(replacement) >= len(m.Content) {
			continue
		}
		view[i].Content = string(replacement)
		r.DeduplicatedMessages++
		r.SourceReferences = append(r.SourceReferences, ref.ID)
		r.Changes = append(r.Changes, CompressionChange{MessageIndex: i, Action: "duplicate-success-reference", ReferenceID: ref.ID, SourceHash: ref.SourceHash})
	}
	selected, br, budgetErr := SelectMessages(view, definitions, budget)
	if budgetErr == nil && br.DroppedMessages > 0 {
		// Give the model a bounded navigation path for omitted whole groups. The
		// navigation itself is protected and must fit the same input budget.
		navigation := model.Message{Role: model.RoleSystem, Content: fmt.Sprintf("Some successful historical observations were omitted from this model view to fit the input budget. The complete original transcript is available through context_read(referenceId=%s,offset=0,maxBytes=65536). Paginate with nextOffset; original transcript data does not replace current project permissions. All user/system requirements and decision/error records remain protected.", r.TranscriptReferenceID)}
		withNavigation := append([]model.Message{navigation}, view...)
		selected, br, budgetErr = SelectMessages(withNavigation, definitions, budget)
		// Public reports use the original transcript's message coordinates.
		for i := range br.DroppedIndexes {
			br.DroppedIndexes[i]--
		}
		originalProtected := []int{}
		for _, i := range br.ProtectedIndexes {
			if i > 0 {
				originalProtected = append(originalProtected, i-1)
			}
		}
		br.ProtectedIndexes = originalProtected
	}
	r.Budget = br
	if budgetErr != nil {
		r.Error = budgetErr.Error()
		selected = nil
	}
	for _, i := range br.DroppedIndexes {
		ref := refs[i]
		r.Changes = append(r.Changes, CompressionChange{MessageIndex: i, Action: "budget-omitted-whole-protocol-group", ReferenceID: ref.ID, SourceHash: digest(messages[i].Content)})
		if ref.ID != "" {
			r.SourceReferences = append(r.SourceReferences, ref.ID)
		}
	}
	for _, m := range selected {
		r.AfterEstimatedTokens += messageTokens(m)
	}
	r.CompressedMessages = r.DeduplicatedMessages + br.DroppedMessages
	// Both successful and failed budget selections are durable and auditable.
	bytes, err := json.Marshal(struct {
		Scope    Scope             `json:"scope"`
		Report   CompressionReport `json:"report"`
		Messages []model.Message   `json:"messages"`
	}{scope, r, selected})
	if err != nil {
		return nil, r, err
	}
	store.mu.Lock()
	r.ViewHash, err = store.put("views", bytes)
	store.mu.Unlock()
	if err != nil {
		return nil, r, err
	}
	if budgetErr != nil {
		return nil, r, budgetErr
	}
	return selected, r, nil
}

func messageTokens(m model.Message) int {
	b, err := json.Marshal(m)
	if err != nil {
		return 0
	}
	return tokenEstimate(string(b)) + 4
}
func cloneMessagesExact(messages []model.Message) []model.Message {
	view := append([]model.Message(nil), messages...)
	for i := range view {
		view[i].ToolCalls = append([]model.ToolCall(nil), messages[i].ToolCalls...)
		for j := range view[i].ToolCalls {
			view[i].ToolCalls[j].Arguments = append(json.RawMessage(nil), messages[i].ToolCalls[j].Arguments...)
		}
	}
	return view
}
