package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectInvalidInputWithoutNetworkOrCredentialEcho(t *testing.T) {
	for _, input := range []string{
		`{"name":"docs","url":"https://example.com","bearer":"fixture-secret","command":"arbitrary"}`,
		`{} {"name":"second"}`,
		`{"name":"docs"}` + strings.Repeat(" ", 32<<10) + `{"url":"hidden"}`,
		`{`,
	} {
		var output bytes.Buffer
		run(strings.NewReader(input), &output)
		var report struct {
			OK   bool   `json:"ok"`
			Code string `json:"code"`
		}
		if json.Unmarshal(output.Bytes(), &report) != nil || report.OK || report.Code != "configuration" || strings.Contains(output.String(), "fixture-secret") {
			t.Fatalf("invalid input not safely rejected: %s", &output)
		}
	}
}
