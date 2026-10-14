package v2input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestExposureClosureFreezesCompleteInventory(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	ev := .5
	params := exposureParams{1, uuid.New(), SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}, ref(), ref(), ref(), ref(), "local_exposure", .8, &ev, nil, true}
	child := uuid.New()
	task := uuid.NewSHA1(child, []byte("semantic.edit:"+params.TargetID.String()))
	paramsRef := Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params"))}
	raw, _ := canonical.Marshal(params)
	paramsRef.SHA256, _ = canonical.Hash(json.RawMessage(raw))
	alpha := ref()
	deps := ExposureDependencies{Reference: ref(), Source: CaptureImageInputs{Pixels: ref(), Alpha: &alpha}, ReferenceImage: CaptureImageInputs{Pixels: ref()}, MaskPixels: ref(), Protection: ref(), ProtectionPixels: ref(), Geometry: []Ref{ref()}}
	id, frozen, err := CompileExposureClosure(child, raw, paramsRef, deps)
	if err != nil || id != task {
		t.Fatal("compile", err)
	}
	validDeps := deps
	validDeps.Geometry = append([]Ref(nil), deps.Geometry...)
	validAlpha := alpha
	validDeps.Source.Alpha = &validAlpha
	raw[0] = '!'
	deps.Geometry[0] = Ref{}
	alpha = Ref{}
	cursor, err := frozen.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]Entry{}
	for {
		entry, more, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if !validEntry(entry) {
			t.Fatal("mutated frozen inventory")
		}
		seen[entry.Ref.ArtifactID] = entry
	}
	if len(seen) != 14 || len(frozen.Direct) != 14 || frozen.ParamsJCS[0] != '{' || len(frozen.InputDigest) != 64 {
		t.Fatal("incomplete inventory", len(seen))
	}
	if seen[params.Transform.ArtifactID].Contract != "ResolvedTransform" {
		t.Fatal("transform contract")
	}
	// Test each independently modified byte document with a matching new hash;
	// these failures must come from the contract, not only hash mismatch.
	validRaw, _ := canonical.Marshal(params)
	if _, _, err := CompileExposureClosure(child, validRaw, paramsRef, validDeps); err != nil {
		t.Fatal("negative-test baseline", err)
	}
	for _, mutation := range []func(map[string]any){
		func(p map[string]any) { p["kind"] = "remove_object" },
		func(p map[string]any) { p["strength"] = nil },
		func(p map[string]any) { p["ev"] = 1.1 },
		func(p map[string]any) { p["preserve_persons"] = false },
		func(p map[string]any) { delete(p, "prompt") },
		func(p map[string]any) { p["prompt"] = "" },
		func(p map[string]any) { p["source"].(map[string]any)["canvas_hash"] = nil },
	} {
		var p map[string]any
		json.Unmarshal(validRaw, &p)
		mutation(p)
		body, _ := canonical.Marshal(p)
		pin := paramsRef
		pin.SHA256, _ = canonical.Hash(json.RawMessage(body))
		if _, _, err := CompileExposureClosure(child, body, pin, validDeps); err == nil {
			t.Fatal("invalid contract accepted")
		}
	}
	if _, _, err := CompileExposureClosure(uuid.New(), validRaw, paramsRef, validDeps); err == nil {
		t.Fatal("different child accepted")
	}
	missing := validDeps
	missing.ProtectionPixels = Ref{}
	if _, _, err := CompileExposureClosure(child, validRaw, paramsRef, missing); err == nil {
		t.Fatal("missing protection pixels accepted")
	}
	conflict := validDeps
	conflict.ProtectionPixels = validDeps.MaskPixels
	conflict.ProtectionPixels.SHA256 = strings.Repeat("c", 64)
	if _, _, err := CompileExposureClosure(child, validRaw, paramsRef, conflict); err == nil {
		t.Fatal("conflicting artifact hash accepted")
	}
}
