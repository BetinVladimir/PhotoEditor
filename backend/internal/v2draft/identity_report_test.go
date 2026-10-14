package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func identityFixture() (map[string]any, v2input.Ref) {
	_, parent := imageResultFixture()
	return map[string]any{"schema_version": 1, "status": "not_measurable", "faces": []any{map[string]any{"face_id": uuid.New(), "similarity": nil, "landmark_drift": nil}}, "changed_mask": parent, "generated_mask": nil, "outside_mask_max_error": 0, "calibration": parent}, parent
}

func TestIdentityReportTypedMasksCalibrationAndNullableMetrics(t *testing.T) {
	fields, parent := identityFixture()
	raw, _ := canonical.Marshal(fields)
	refs, err := identityReportRefs(raw, "identity", parent)
	if err != nil || len(refs) != 2 || refs[0].Contract != "MaskRef" || refs[1].Contract != "Calibration" || refs[0].ParentRef != parent {
		t.Fatal(refs, err)
	}
	checkCommonIdentityChildren(t, raw, parent)
	fields["generated_mask"] = parent
	fields["faces"] = []any{map[string]any{"face_id": uuid.New(), "similarity": -.5, "landmark_drift": .2}}
	raw, _ = canonical.Marshal(fields)
	refs, err = identityReportRefs(raw, "identity", parent)
	if err != nil || len(refs) != 3 || refs[1].Path != "identity.generated_mask" {
		t.Fatal(refs, err)
	}
	checkCommonIdentityChildren(t, raw, parent)
}

func checkCommonIdentityChildren(t *testing.T, raw []byte, parent v2input.Ref) {
	t.Helper()
	expected, err := identityReportRefs(raw, "Document", parent)
	if err != nil {
		t.Fatal(err)
	}
	children, err := DocumentChildren(raw, "IdentityReport", parent)
	if err != nil || !reflect.DeepEqual(children, expected) {
		t.Fatal("common reader changed typed identity child order/path/ref/parent", children, expected, err)
	}
}

func TestIdentityReportRejectsMalformedMetricsAndRefs(t *testing.T) {
	mutations := []func(map[string]any){
		func(f map[string]any) { delete(f, "generated_mask") }, func(f map[string]any) { f["extra"] = true },
		func(f map[string]any) { f["status"] = "unavailable" }, func(f map[string]any) { f["faces"] = nil },
		func(f map[string]any) { f["outside_mask_max_error"] = nil }, func(f map[string]any) { f["changed_mask"] = nil },
		func(f map[string]any) { f["calibration"] = map[string]any{} },
		func(f map[string]any) {
			f["faces"] = []any{map[string]any{"face_id": uuid.New(), "similarity": 1.01, "landmark_drift": nil}}
		},
		func(f map[string]any) {
			f["faces"] = []any{map[string]any{"face_id": uuid.New(), "similarity": nil, "landmark_drift": -0.1}}
		},
		func(f map[string]any) {
			f["faces"] = []any{map[string]any{"face_id": nil, "similarity": nil, "landmark_drift": nil}}
		},
		func(f map[string]any) { f["faces"] = make([]any, 101) },
	}
	for i, mutate := range mutations {
		fields, parent := identityFixture()
		mutate(fields)
		raw, _ := canonical.Marshal(fields)
		if _, err := identityReportRefs(raw, "identity", parent); err == nil {
			t.Fatalf("accepted %d", i)
		}
		if _, err := DocumentChildren(raw, "IdentityReport", parent); err == nil {
			t.Fatalf("common reader accepted malformed identity report %d", i)
		}
	}
}

func TestIdentityReportStatusMatchesMeasuredEvidence(t *testing.T) {
	fields, parent := identityFixture()
	faceID := uuid.New()
	fields["status"] = "pass"
	fields["faces"] = []any{map[string]any{"face_id": faceID, "similarity": 0.91, "landmark_drift": 0.02}}
	raw, _ := canonical.Marshal(fields)
	if _, err := DocumentChildren(raw, "IdentityReport", parent); err != nil {
		t.Fatal("measured pass rejected", err)
	}
	for name, change := range map[string]func(){
		"empty pass": func() { fields["faces"] = []any{} },
		"null pass metric": func() {
			fields["faces"] = []any{map[string]any{"face_id": faceID, "similarity": nil, "landmark_drift": 0.02}}
		},
		"outside error pass": func() { fields["outside_mask_max_error"] = 0.001 },
		"duplicate face": func() {
			fields["faces"] = []any{map[string]any{"face_id": faceID, "similarity": 0.91, "landmark_drift": 0.02}, map[string]any{"face_id": faceID, "similarity": 0.91, "landmark_drift": 0.02}}
		},
		"zero face": func() {
			fields["faces"] = []any{map[string]any{"face_id": uuid.Nil, "similarity": 0.91, "landmark_drift": 0.02}}
		},
	} {
		fields, parent = identityFixture()
		fields["status"] = "pass"
		fields["faces"] = []any{map[string]any{"face_id": faceID, "similarity": 0.91, "landmark_drift": 0.02}}
		change()
		raw, _ = canonical.Marshal(fields)
		if _, err := DocumentChildren(raw, "IdentityReport", parent); err == nil {
			t.Fatal(name, "accepted")
		}
	}
	fields, parent = identityFixture()
	fields["status"] = "fail"
	fields["faces"] = []any{}
	fields["outside_mask_max_error"] = 0.1
	raw, _ = canonical.Marshal(fields)
	if _, err := DocumentChildren(raw, "IdentityReport", parent); err != nil {
		t.Fatal("numeric guard failure rejected", err)
	}
}
