package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Credentials are retrieved after claiming a lease, never from the broker payload.
type modelConnection struct {
	BaseURL  string          `json:"baseUrl"`
	APIKey   string          `json:"apiKey"`
	WireAPI  string          `json:"wireApi"`
	Settings *model.Settings `json:"settings"`
}

func (r *runner) resolveModel(ctx context.Context, task *taskMessage) error {
	if task.ProviderID == "" {
		return nil
	}
	response, err := r.request(ctx, http.MethodGet, "/internal/tasks/"+task.TaskID+"/model-connection?attempt="+strconv.Itoa(task.Attempt)+"&runnerId="+url.QueryEscape(r.ID), nil)
	if err != nil {
		return errors.New("model connection request failed")
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("model connection returned HTTP %d", response.StatusCode)
	}
	var value modelConnection
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil {
		return errors.New("invalid model connection response")
	}
	u, err := url.Parse(value.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || value.APIKey == "" || (value.WireAPI != "chat" && value.WireAPI != "responses" && value.WireAPI != "anthropic") {
		return errors.New("invalid model connection configuration")
	}
	if value.Settings != nil {
		if err := value.Settings.Validate(); err != nil {
			return err
		}
	}
	task.connection = &value
	return nil
}

func (r *runner) modelProvider(task taskMessage) *model.OpenAICompatible {
	base, key, wire := r.ModelBaseURL, r.ModelAPIKey, r.ModelWireAPI
	if task.connection != nil {
		base, key, wire = task.connection.BaseURL, task.connection.APIKey, task.connection.WireAPI
	}
	client := r.ModelHTTP
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	var settings *model.Settings
	if task.connection != nil {
		settings = task.connection.Settings
	}
	return &model.OpenAICompatible{Settings: settings, BaseURL: base, APIKey: key, WireAPI: wire, Client: &copyClient}
}
