package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/agent"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type taskInstruction struct {
	TaskID   string `json:"taskId"`
	Attempt  int    `json:"attempt"`
	Sequence int64  `json:"sequence"`
	Content  string `json:"content"`
}

func (r *runner) instructionPoller(task taskMessage, saved *workspaceMetadata, persist func() error) func(context.Context, agent.RunResult) ([]model.Message, error) {
	var acknowledged int64
	path := "/internal/tasks/" + task.TaskID + "/instructions"
	query := "?attempt=" + strconv.Itoa(task.Attempt) + "&runnerId=" + url.QueryEscape(r.ID)
	ack := func(ctx context.Context) error {
		if saved.InstructionSequence <= acknowledged {
			return nil
		}
		response, err := r.request(ctx, http.MethodPost, path+"/ack"+query, map[string]any{"through": saved.InstructionSequence})
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode/100 != 2 {
			return fmt.Errorf("instruction acknowledgement returned HTTP %d", response.StatusCode)
		}
		acknowledged = saved.InstructionSequence
		return nil
	}
	return func(ctx context.Context, state agent.RunResult) ([]model.Message, error) {
		messages := append([]model.Message(nil), state.Messages...)
		if err := ack(ctx); err != nil {
			return messages, err
		}
		response, err := r.request(ctx, http.MethodGet, path+query+"&after="+strconv.FormatInt(saved.InstructionSequence, 10), nil)
		if err != nil {
			return messages, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return messages, fmt.Errorf("instructions returned HTTP %d", response.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
		if err != nil || len(raw) > 1<<20 {
			return messages, errors.New("instruction queue exceeds response limit")
		}
		var instructions []taskInstruction
		if json.Unmarshal(raw, &instructions) != nil || len(instructions) > 100 {
			return messages, errors.New("invalid instruction queue")
		}
		cursor := saved.InstructionSequence
		for _, instruction := range instructions {
			if instruction.TaskID != task.TaskID || instruction.Attempt != task.Attempt || instruction.Sequence != cursor+1 || instruction.Content == "" || len([]rune(instruction.Content)) > 16000 {
				return state.Messages, errors.New("instruction scope, sequence or content is invalid")
			}
			messages = append(messages, model.Message{Role: model.RoleUser, Content: fmt.Sprintf("Additional user instruction #%d (applies from this point; does not revoke tool approvals):\n%s", instruction.Sequence, instruction.Content)})
			cursor = instruction.Sequence
		}
		if cursor == saved.InstructionSequence {
			return messages, nil
		}
		// Cursor and full canonical messages are committed together, before acknowledging
		// delivery. A process interruption cannot lose an acknowledged instruction.
		previous := *saved
		saved.Messages, saved.Usage, saved.InstructionSequence = messages, state.Usage, cursor
		if err := persist(); err != nil {
			*saved = previous
			return state.Messages, err
		}
		if err := ack(ctx); err != nil {
			return messages, err
		}
		return messages, nil
	}
}
