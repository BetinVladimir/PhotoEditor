package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationPreviewArrayLocatorRequiresExactClosedObject(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	valid := map[string]any{"key": "tenants/a/pixels", "version": "v1",
		"sha256": ref.SHA256, "bytes": int64(123), "media_type": "application/x-npy"}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := parseRestorationPreviewArrayLocator(raw, ref)
	if err != nil || obj.Key != valid["key"] || obj.VersionID != "v1" {
		t.Fatal(err)
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["sha256"] = strings.Repeat("b", 64) },
		func(v map[string]any) { v["version"] = "null" },
		func(v map[string]any) { v["media_type"] = "image/png" },
		func(v map[string]any) { v["extra"] = true },
		func(v map[string]any) { v["bytes"] = int64(0) },
	} {
		copy := make(map[string]any, len(valid))
		for k, v := range valid {
			copy[k] = v
		}
		change(copy)
		body, err := canonical.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseRestorationPreviewArrayLocator(body, ref); err == nil {
			t.Fatal("invalid locator accepted", copy)
		}
	}
}

func TestRestorationPreviewArrayWitnessRequiresBothSides(t *testing.T) {
	plan := restorationPreviewFixturePlan(t)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	obj := objectstore.Object{Key: "tenants/a/pixels", VersionID: "v1",
		SHA256: ref.SHA256, Bytes: 123, ContentType: "application/x-npy"}
	verified := VerifiedRestorationPreviewInputs{source: plan.source,
		arrays: []RestorationPreviewArrayWitness{
			{Side: "before", Channel: "pixels", Ref: ref, Object: obj},
			{Side: "after", Channel: "pixels", Ref: ref, Object: obj},
		}}
	if _, err := verified.AdmissionJSON(); err != nil {
		t.Fatal(err)
	}
	verified.arrays[1].Side = "before"
	if _, err := verified.AdmissionJSON(); err == nil {
		t.Fatal("duplicate side accepted")
	}
	verified.arrays = verified.arrays[:1]
	if _, err := verified.AdmissionJSON(); err == nil {
		t.Fatal("missing after accepted")
	}
}
