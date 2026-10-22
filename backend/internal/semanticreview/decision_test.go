package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSelectionBindsRevisionPlanAndOnlyExplicitTargets(t *testing.T) {
	id, omitted := uuid.New(), uuid.New()
	decision := ApplyDecision{strings.Repeat("a", 64), []TargetDecision{{TargetID: id, Decision: "approve"}}, "1"}
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
	d := ApplyDecision{strings.Repeat("a", 64), []TargetDecision{{TargetID: uuid.New(), Decision: "reject"}}, "1"}
	if !validRevision("9007199254740991") {
		t.Fatal("safe maximum revision rejected")
	}
	raw, _ := canonical.Marshal(d)
	if _, err := DecodeApplyDecision(raw); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "01", "-1", "9007199254740991", "9007199254740992", "9223372036854775808"} {
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


func TestDecodeShownSelectionRequiresDistinctExactReferencesAndExplicitApproval(t *testing.T) {
	for _, mode := range []string{"valid", "reject_with_preview", "nil_render", "nil_presentation", "alias", "render_sha", "presentation_sha", "unknown_preview", "duplicate_render"} {
		t.Run(mode, func(t *testing.T) {
			decision := ApplyDecision{PlanSHA: strings.Repeat("a",64), ExpectedRevision: "1", TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "approve", Preview: &PreviewSelection{
				Render: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("a",64)},
				Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: strings.Repeat("b",64)}}}}}
			item := &decision.TargetDecisions[0]
			switch mode {
			case "reject_with_preview": item.Decision = "reject"
			case "nil_render": item.Preview.Render.ArtifactID = uuid.Nil
			case "nil_presentation": item.Preview.Evidence.ArtifactID = uuid.Nil
			case "alias": item.Preview.Evidence.ArtifactID = item.Preview.Render.ArtifactID
			case "render_sha": item.Preview.Render.SHA256 = "bad"
			case "presentation_sha": item.Preview.Evidence.SHA256 = strings.Repeat("A",64)
			}
			raw, err := canonical.Marshal(decision)
			if err != nil { t.Fatal(err) }
			if mode == "unknown_preview" { raw = []byte(strings.Replace(string(raw), `"preview":{`, `"preview":{"approve_automatically":true,`,1)) }
			if mode == "duplicate_render" { raw = []byte(strings.Replace(string(raw), `"preview":{`, `"preview":{"render":null,`,1)) }
			got, err := DecodeApplyDecision(raw)
			if (err == nil) != (mode == "valid") { t.Fatal("invalid shown selection transport", mode, err) }
			if mode == "valid" && (got.TargetDecisions[0].Preview.Render != item.Preview.Render || got.TargetDecisions[0].Preview.Evidence != item.Preview.Evidence) { t.Fatal("shown references changed") }
		})
	}
}

func TestApplyExpectedRevisionLeavesSpaceForAtomicIncrement(t *testing.T) {
	decision := ApplyDecision{PlanSHA: strings.Repeat("a", 64), ExpectedRevision: "9007199254740990",
		TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: "reject"}}}
	raw, err := canonical.Marshal(decision)
	if err != nil { t.Fatal(err) }
	if _, err := DecodeApplyDecision(raw); err != nil { t.Fatal("last incrementable revision refused", err) }
	decision.ExpectedRevision = "9007199254740991"
	raw, err = canonical.Marshal(decision)
	if err != nil { t.Fatal(err) }
	if _, err := DecodeApplyDecision(raw); err == nil { t.Fatal("exhausted revision accepted for mutation") }
	if !validRevision(decision.ExpectedRevision) { t.Fatal("maximum saved/read revision must remain valid") }
}


func TestApplyDecisionDistinguishesOmittedAndNullPreview(t *testing.T) {
	for _, decision := range []string{"reject", "approve"} {
		t.Run(decision, func(t *testing.T) {
			request := ApplyDecision{PlanSHA: strings.Repeat("a", 64), ExpectedRevision: "1",
				TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: decision}}}
			raw, err := canonical.Marshal(request)
			if err != nil { t.Fatal(err) }
			if _, err := DecodeApplyDecision(raw); err != nil { t.Fatal("omitted preview refused", err) }
			for _, value := range []string{"null", " null "} {
				invalid := strings.Replace(string(raw), `"decision":"`+decision+`"`, `"preview":`+value+`,"decision":"`+decision+`"`, 1)
				if _, err := DecodeApplyDecision([]byte(invalid)); err != ErrInvalid {
					t.Fatal("explicit null preview accepted", value, err)
				}
			}
		})
	}
}
