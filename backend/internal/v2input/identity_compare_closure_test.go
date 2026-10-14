package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func identityClosureFixture(t *testing.T) (IdentityCompareParams, Ref, IdentityCompareDocuments) {
	t.Helper()
	data, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(data, &images); err != nil || len(images) == 0 {
		t.Fatal("image fixture", err)
	}
	before, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	after := before
	after.Pixels.ID = uuid.New()
	after.Pixels.SHA256 = strings.Repeat("e", 64)
	encode := func(value any) json.RawMessage {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	ref := func(body []byte) Ref {
		sum := sha256.Sum256(body)
		return Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	beforeBody, afterBody := encode(before), encode(after)
	canvasHash, err := canonical.Hash(before.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	effective := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Canvas: before.Canvas, TransformChain: before.TransformChain, Selector: "manual", Confidence: 1, Protected: false}
	protected := effective
	protected.Artifact.ID = uuid.New()
	protected.Artifact.SHA256 = strings.Repeat("1", 64)
	protected.Protected = true
	effectiveBody, protectedBody := encode(effective), encode(protected)
	calibration := encode(map[string]any{"schema_version": 1, "model_lock_sha": strings.Repeat("d", 64), "qualified": true})
	consents := encode(map[string]any{"schema_version": 1, "grants": []any{}, "confirmed_faces": []any{}})
	p := IdentityCompareParams{SchemaVersion: 1,
		Before:        SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(beforeBody), CanvasHash: canvasHash},
		After:         SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(afterBody), CanvasHash: canvasHash},
		EffectiveMask: ref(effectiveBody), Protected: ref(protectedBody), ModelLockSHA: strings.Repeat("d", 64), Calibration: ref(calibration), Consents: ref(consents)}
	paramsBody, _, err := CompileIdentityCompareParams(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, ref(paramsBody), IdentityCompareDocuments{Before: beforeBody, After: afterBody, EffectiveMask: effectiveBody, Protected: protectedBody, Calibration: calibration, Consents: consents}
}

func TestIdentityCompareClosureDeclaresExactChildren(t *testing.T) {
	p, paramsRef, docs := identityClosureFixture(t)
	ctx := context.Background()
	closure, err := CompileIdentityCompareClosure(ctx, uuid.New(), 0, p, paramsRef, docs)
	if err != nil || closure.Open == nil || len(closure.Direct) < 11 || closure.InputDigest == "" {
		t.Fatal("closure", err)
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[uuid.UUID]Entry{}
	for {
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		entries[entry.Ref.ArtifactID] = entry
	}
	for _, item := range []struct {
		ref      Ref
		contract string
	}{{p.Before.Source, "WorkingImage"}, {p.After.Source, "WorkingImage"}, {p.EffectiveMask, "MaskRef"}, {p.Protected, "MaskRef"}, {p.Calibration, "Calibration"}, {p.Consents, "ConsentSnapshot"}, {paramsRef, "Params"}} {
		if entries[item.ref.ArtifactID] != (Entry{Ref: item.ref, Contract: item.contract}) {
			t.Fatal("missing declared contract", item.contract)
		}
	}
	for _, raw := range []json.RawMessage{docs.Before, docs.After} {
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			t.Fatal(err)
		}
		if entries[image.Pixels.ID].Contract != "NpyArray" {
			t.Fatal("image pixels omitted")
		}
	}
	for _, raw := range []json.RawMessage{docs.EffectiveMask, docs.Protected} {
		mask, err := review.ParseMaskRef(raw)
		if err != nil {
			t.Fatal(err)
		}
		if entries[mask.Artifact.ID].Contract != "NpyArray" {
			t.Fatal("mask pixels omitted")
		}
	}
}

func TestIdentityCompareClosureRefusesSubstitution(t *testing.T) {
	p, paramsRef, docs := identityClosureFixture(t)
	for name, mutate := range map[string]func(*IdentityCompareDocuments){
		"after bytes":   func(d *IdentityCompareDocuments) { d.After = append(append([]byte(nil), d.After...), ' ') },
		"consent bytes": func(d *IdentityCompareDocuments) { d.Consents = append(append([]byte(nil), d.Consents...), ' ') },
		"mask swap":     func(d *IdentityCompareDocuments) { d.Protected = d.EffectiveMask },
	} {
		changed := docs
		mutate(&changed)
		if _, err := CompileIdentityCompareClosure(context.Background(), uuid.New(), 0, p, paramsRef, changed); err == nil {
			t.Fatal(name)
		}
	}
	paramsRef.SHA256 = strings.Repeat("0", 64)
	if _, err := CompileIdentityCompareClosure(context.Background(), uuid.New(), 0, p, paramsRef, docs); err == nil {
		t.Fatal("foreign Params accepted")
	}
}
