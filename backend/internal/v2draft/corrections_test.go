package v2draft

import (
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestCorrectionPlanRequiresExactCurrentBinding(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	item := map[string]any{"binding": binding, "ordinal": 0,
		"delta": map[string]any{"ev": 0.25, "log_r": 0, "log_g": 0,
			"log_b": 0, "saturation_delta": 0, "retouch_delta": 0},
		"disposition": "apply", "reason": nil}
	plan := map[string]any{"schema_version": 1,
		"snapshot_digest": strings.Repeat("c", 64), "items": []any{item}}
	body, err := canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorrectionPlan(body, binding); err != nil {
		t.Fatal(err)
	}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	children, err := DocumentOccurrenceChildren(body, LayerRef{Path: "TaskInputs[0].recipe.session_corrections", Contract: "CorrectionPlan", Ref: parent})
	if err != nil || len(children) != 1 || children[0].Ref != binding.Source || children[0].ParentRef != parent || children[0].Contract != "WorkingImage" || children[0].CanvasHash != binding.CanvasHash || children[0].Path != "TaskInputs[0].recipe.session_corrections.items[0].binding.source" {
		t.Fatal("correction children lost typed parent/canvas", children, err)
	}
	delta, err := CurrentCorrection(body, binding)
	if err != nil || delta.EV != 0.25 {
		t.Fatalf("wrong current correction: %+v, %v", delta, err)
	}
	item["disposition"] = "manual_locked"
	body, err = canonical.Marshal(plan)
	if err != nil || ValidateCorrectionPlan(body, binding) == nil {
		t.Fatal("accepted nonzero locked correction")
	}
	item["disposition"] = "apply"
	binding.AssetVersionID = uuid.New()
	if err := ValidateCorrectionPlan(body, binding); err == nil {
		t.Fatal("accepted plan without current asset")
	}
	binding = item["binding"].(v2input.SessionVersionBinding)
	item["ordinal"] = 5000
	body, err = canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorrectionPlan(body, binding); err == nil {
		t.Fatal("accepted ordinal outside snapshot bounds")
	}
}

func TestCorrectionVectorSumRejectsOutOfRangeResult(t *testing.T) {
	first := CorrectionVector{EV: 0.75, LogR: 0.1}
	second := CorrectionVector{EV: 0.26, LogR: 0.12}
	if _, err := SumCorrectionVectors(first, second); err == nil {
		t.Fatal("accepted an effective exposure over the contract bound")
	}
	second.EV = -0.25
	got, err := SumCorrectionVectors(first, second)
	if err != nil || got.EV != 0.5 || math.Abs(got.LogR-0.22) > 1e-12 {
		t.Fatalf("wrong bounded sum: %+v, %v", got, err)
	}
}

func TestCorrectionPlanRetainsNoncontiguousSceneSnapshotOrdinals(t *testing.T) {
 for _, mode := range []string{"valid", "duplicate", "descending", "negative", "overflow", "repeated_version"} { t.Run(mode, func(t *testing.T) {
  binding := func() v2input.SessionVersionBinding { return v2input.SessionVersionBinding{AssetVersionID:uuid.New(), Source:v2input.Ref{ArtifactID:uuid.New(), SHA256:strings.Repeat("a",64)}, CanvasHash:strings.Repeat("b",64)} }
  first, second := binding(), binding()
  ordinals := []int{2,7}
  switch mode {
  case "duplicate": ordinals[1] = 2
  case "descending": ordinals = []int{7,2}
  case "negative": ordinals[0] = -1
  case "overflow": ordinals[1] = 5000
  case "repeated_version": second = first
  }
  items := []any{}
  for index, source := range []v2input.SessionVersionBinding{first,second} {
   items = append(items,map[string]any{"binding":source,"ordinal":ordinals[index],"delta":CorrectionVector{EV:0.25},"disposition":"apply","reason":nil})
  }
  raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":strings.Repeat("c",64),"items":items});if err!=nil{t.Fatal(err)}
  err=ValidateCorrectionPlan(raw,second)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected scene ordinal validation: %v",err)}
  if mode=="valid" {
   current,err:=CurrentCorrection(raw,second);if err!=nil||current.EV!=0.25{t.Fatal("lost correction for snapshot ordinal7",err)}
   refs,err:=DocumentChildren(raw,"CorrectionPlan",v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)})
   if err!=nil||len(refs)!=2||refs[1].Ref!=second.Source||refs[1].Path!="Document.items[1].binding.source"{t.Fatal("occurrence index confused with snapshot ordinal",err)}
  }
 }) }
}
