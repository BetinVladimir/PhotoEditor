package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDecodeMultiApplyDecisionStrictContract(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	good := MultiApplyDecision{SchemaVersion: 1, ResultID: uuid.New(), Root: ref, QualityRun: uuid.New(), QualityResult: uuid.New(), QualityReport: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, ConfirmGeometryReset: true}
	raw, err := canonical.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMultiApplyDecision(raw)
	if err != nil || got != good {
		t.Fatal(got, err)
	}
	for _, change := range []func(*MultiApplyDecision){
		func(v *MultiApplyDecision) { v.SchemaVersion = 2 },
		func(v *MultiApplyDecision) { v.ResultID = uuid.Nil },
		func(v *MultiApplyDecision) { v.Root.SHA256 = "changed" },
		func(v *MultiApplyDecision) { v.ConfirmGeometryReset = false },
		func(v *MultiApplyDecision) { v.QualityReport.ArtifactID = uuid.Nil },
	} {
		bad := good
		change(&bad)
		body, err := canonical.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeMultiApplyDecision(body); err == nil {
			t.Fatal("invalid decision admitted")
		}
	}
	for _, body := range [][]byte{nil, []byte(`{}`), append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"extra":true}`)...), append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"schema_version":1}`)...)} {
		if _, err := DecodeMultiApplyDecision(body); err == nil {
			t.Fatal("invalid body admitted")
		}
	}
}
