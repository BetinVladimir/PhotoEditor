package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSelectionBindsRevisionPlanAndOnlyExplicitTargets(t *testing.T) {
	id, omitted := uuid.New(), uuid.New()
	decision := ApplyDecision{strings.Repeat("a", 64), []TargetDecision{{id, "approve"}}, "1"}
	states := map[uuid.UUID]string{id: "proposed", omitted: "proposed"}
	selected, err := ValidateSelection(decision, decision.PlanSHA, "1", states)
	if err != nil || len(selected) != 1 || selected[0].TargetID != id {
		t.Fatal("explicit selection failed")
	}
	selected[0].Decision = "reject"
	if decision.TargetDecisions[0].Decision != "approve" {
		t.Fatal("selection alias")
	}
	for _, variant := range []string{"revision", "plan", "missing", "unsupported", "duplicate"} {
		invalid := decision
		invalid.TargetDecisions = append([]TargetDecision(nil), decision.TargetDecisions...)
		scope := map[uuid.UUID]string{id: "proposed"}
		sha, revision := decision.PlanSHA, "1"
		switch variant {
		case "revision":
			revision = "2"
		case "plan":
			sha = strings.Repeat("b", 64)
		case "missing":
			delete(scope, id)
			scope[omitted] = "proposed"
		case "unsupported":
			scope[id] = "unsupported"
		case "duplicate":
			invalid.TargetDecisions = append(invalid.TargetDecisions, invalid.TargetDecisions[0])
		}
		if _, err := ValidateSelection(invalid, sha, revision, scope); err == nil {
			t.Fatal(variant)
		}
	}
}
func TestDecodeRejectsAmbiguousAndInvalidRevision(t *testing.T) {
	d := ApplyDecision{strings.Repeat("a", 64), []TargetDecision{{uuid.New(), "reject"}}, "1"}
	if !validRevision("9007199254740991") {
		t.Fatal("safe maximum revision rejected")
	}
	raw, _ := canonical.Marshal(d)
	if _, err := DecodeApplyDecision(raw); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "01", "-1", "9007199254740992", "9223372036854775808"} {
		d.ExpectedRevision = revision
		body, _ := canonical.Marshal(d)
		if _, err := DecodeApplyDecision(body); err == nil {
			t.Fatal(revision)
		}
	}
	ambiguous := append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"plan_sha":"`+strings.Repeat("b", 64)+`"}`)...)
	if _, err := DecodeApplyDecision(ambiguous); err == nil {
		t.Fatal("duplicate key")
	}
}
