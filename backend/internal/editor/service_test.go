package editor

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestValidObjectRequiresObjectAndSchema(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{`{"schema_version":1,"modules":{}}`, true},
		{`{"schema_version":2}`, false},
		{`[]`, false},
		{`null`, false},
		{`{`, false},
	} {
		err := validObject(json.RawMessage(tc.raw), 1024, true)
		if (err == nil) != tc.ok {
			t.Fatalf("validObject(%s): err=%v", tc.raw, err)
		}
	}
}

func TestSHA256Pattern(t *testing.T) {
	if !sha256Pattern.MatchString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatal("valid digest rejected")
	}
	if sha256Pattern.MatchString("G123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatal("invalid digest accepted")
	}
}

func TestValidateOverrideStrictShapeAndSource(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	valid := json.RawMessage(`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"revision":1}`)
	if err := validateOverride(valid, sha); err != nil {
		t.Fatalf("valid override rejected: %v", err)
	}
	for _, raw := range []string{
		`{"schema_version":1,"input_sha256":"` + sha + `","modules":{},"revision":1}`,
		`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"model_path":"x"},"revision":1}`,
		`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"revision":1,"unknown":true}`,
		`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"accepted_candidate_ids":["x"],"rejected_candidate_ids":["x"],"revision":1}`,
	} {
		if err := validateOverride(json.RawMessage(raw), sha); err == nil {
			t.Fatalf("invalid override accepted: %s", raw)
		}
	}
	if err := validateOverride(valid, "f"+sha[1:]); err == nil {
		t.Fatal("wrong source accepted")
	}
}

func TestValidateOverrideMaskBindingsAreTypedAndConfirmed(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mask := `{"artifact":{"artifact_id":"11111111-1111-4111-8111-111111111111","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"canvas":{"width":32,"height":24,"orientation":1,"space_id":"source","source_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"transform_chain":{"steps":[],"hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"object_id":null,"selector":"manual","confidence":1,"model_lock_id":null,"protected":false}`
	valid := json.RawMessage(`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"mask_refs":[{"binding_id":"repair-1","purpose":"inpaint_target","mask":` + mask + `,"confirmed":true,"strength":1}],"revision":1}`)
	if err := validateOverride(valid, sha); err != nil {
		t.Fatalf("valid mask binding rejected: %v", err)
	}
	for _, replacement := range []string{
		`"confirmed":false`,
		`"purpose":"unknown"`,
		`"selector":"person"`,
	} {
		candidate := string(valid)
		switch replacement {
		case `"confirmed":false`:
			candidate = string(bytes.ReplaceAll(valid, []byte(`"confirmed":true`), []byte(replacement)))
		case `"purpose":"unknown"`:
			candidate = string(bytes.ReplaceAll(valid, []byte(`"purpose":"inpaint_target"`), []byte(replacement)))
		case `"selector":"person"`:
			candidate = string(bytes.ReplaceAll(valid, []byte(`"selector":"manual"`), []byte(replacement)))
		}
		if err := validateOverride(json.RawMessage(candidate), sha); err == nil {
			t.Fatalf("invalid mask binding accepted: %s", replacement)
		}
	}
}

func TestValidateOverridePersonSubjectRequiresOriginalConfirmedPersonMask(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mask := `{"artifact":{"artifact_id":"11111111-1111-4111-8111-111111111111","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"canvas":{"width":32,"height":24,"orientation":1,"space_id":"source","source_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"transform_chain":{"steps":[],"hash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},"object_id":null,"selector":"person","confidence":1,"model_lock_id":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","protected":false}`
	valid := json.RawMessage(`{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"mask_refs":[{"binding_id":"person-1","purpose":"person_subject","mask":` + mask + `,"confirmed":true,"strength":1}],"revision":1}`)
	if err := validateOverride(valid, sha); err != nil {
		t.Fatal("confirmed person source rejected", err)
	}
	for _, replacement := range [][2]string{
		{`"confirmed":true`, `"confirmed":false`},
		{`"selector":"person"`, `"selector":"manual"`},
		{`"protected":false`, `"protected":true`},
		{`"model_lock_id":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"`, `"model_lock_id":null`},
	} {
		candidate := bytes.Replace(valid, []byte(replacement[0]), []byte(replacement[1]), 1)
		if err := validateOverride(candidate, sha); err == nil {
			t.Fatal("invalid person source admitted", replacement[1])
		}
	}
}

func TestValidateOverrideManualGeometry(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := `{"schema_version":1,"input_sha256":"` + sha + `","modules":{"strength":0.7},"revision":1,`
	valid := base + `"protected_polygons":[{"kind":"polygon","sequence":0,"mode":"protect","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}]}]}`
	if err := validateOverride(json.RawMessage(valid), sha); err != nil {
		t.Fatal("valid protected polygon refused", err)
	}
	chainHash, err := canonical.Hash(map[string]any{"steps": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	points := make([]map[string]any, 68)
	for i := range points {
		points[i] = map[string]any{"x": 0.5, "y": 0.5, "confidence": 1.0}
	}
	face, err := canonical.Marshal(map[string]any{"object_id": uuid.New(), "points": points, "canvas": map[string]any{"width": 32, "height": 24, "orientation": 1, "space_id": "canonical", "source_sha256": sha}, "transform_chain": map[string]any{"steps": []any{}, "hash": chainHash}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOverride(json.RawMessage(base+`"manual_landmarks":`+string(face)+`}`), sha); err != nil {
		t.Fatal("valid manual landmarks refused", err)
	}
	for _, field := range []string{
		`"protected_polygons":[{"kind":"polygon","sequence":0,"mode":"add","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}]}]`,
		`"protected_polygons":[{"kind":"polygon","sequence":0,"mode":"protect","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}]},{"kind":"polygon","sequence":0,"mode":"protect","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}]}]`,
		`"protected_polygons":[{"kind":"polygon","sequence":0,"mode":"protect","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":0,"y":1}],"unknown":1}]`,
		`"manual_landmarks":{}`,
	} {
		if err := validateOverride(json.RawMessage(base+field+`}`), sha); err == nil {
			t.Fatal("invalid manual geometry accepted", field)
		}
	}
}
