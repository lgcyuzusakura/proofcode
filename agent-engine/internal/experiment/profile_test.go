package experiment

import "testing"

func TestProfilesAreExecutableAndCannotBeRedefined(t *testing.T) {
	for _, group := range []string{"A", "B", "C", "D", "E", "F"} {
		p, err := ForGroup(group)
		if err != nil || p.Validate(group, ProfileVersion) != nil || !p.MandatorySafety {
			t.Fatalf("invalid profile %s: %+v %v", group, p, err)
		}
		if p.ToolsEnabled != (group != "A") || p.DeterministicSafety != (group == "B" || group == "F") || p.JevRequired != (group == "C" || group == "F") || p.JevRouting != p.JevRequired || p.RAGEnabled != (group == "D" || group == "E" || group == "F") || p.ContextCompressionEnabled != (group == "E" || group == "F") || p.FeedbackRetrieval != (group == "F") {
			t.Fatalf("wrong ablation semantics: %+v", p)
		}
		p.MandatorySafety = false
		if p.Validate(group, ProfileVersion) == nil {
			t.Fatal("queue profile removed mandatory safety")
		}
	}
	if _, err := ForGroup("baseline"); err == nil {
		t.Fatal("unknown group accepted")
	}
	p, _ := ForGroup("F")
	if p.Validate("E", ProfileVersion) == nil || p.Validate("F", "future-version") == nil {
		t.Fatal("mismatched experimental profile accepted")
	}
}
