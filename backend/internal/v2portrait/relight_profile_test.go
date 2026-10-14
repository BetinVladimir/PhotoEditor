package v2portrait

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
	"photoeditor/backend/internal/v2input"
)

func TestRelightProfileExactSourceAndReservations(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	canvas, _ := canonical.Hash(image.Canvas)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	p := RelightParams{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}, Depth: ref, Subject: ref, Protected: ref, Calibration: ref, Lights: []RelightLight{{Radius: .5}}}
	cost, _ := LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
	runtime := strings.Repeat("a", 64)
	paramsBody, paramsSHA, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Depth.ArtifactID = uuid.New()
	p.Subject.ArtifactID = uuid.New()
	p.Protected.ArtifactID = uuid.New()
	paramsBody, paramsSHA, err = p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	pin := RelightQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: runtime,
		Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: runtime}, Calibration: p.Calibration,
		ModelLockSHA: runtime, RuntimeLockSHA: runtime, DeviceProfileSHA: runtime, DatasetSHA: runtime, IssuerID: uuid.New(), Backend: "cpu"}
	roots, err := RelightRootInputs(p, pin)
	if err != nil || len(roots) != 6 {
		t.Fatal("root inventory", err)
	}
	roles := map[string]int{}
	for i, entry := range roots {
		roles[entry.Contract]++
		if i > 0 && roots[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
			t.Fatal("unsorted roots")
		}
	}
	if roles["MaskRef"] != 2 || roles["QualificationReport"] != 1 || roles["RelativeDepth"] != 1 {
		t.Fatal("root contracts")
	}
	alias := p
	alias.Subject = alias.Protected
	if _, err := RelightRootInputs(alias, pin); err == nil {
		t.Fatal("mask role alias")
	}
	badReport := pin
	badReport.Report = p.Base.Source
	if _, err := RelightRootInputs(p, badReport); err == nil {
		t.Fatal("report source alias")
	}
	pinned, err := CompilePinnedRelightTaskPlan(context.Background(), p.Base, raw, paramsBody, runtime, cost, pin)
	if err != nil {
		t.Fatal("pinned plan", err)
	}
	var frozen struct {
		Pin RelightQualificationPin `json:"relight_qualification"`
	}
	if json.Unmarshal(pinned.Profile, &frozen) != nil || frozen.Pin != pin || pinned.ParamsSHA != paramsSHA || pinned.ReservedBytes != cost.OutputBytes {
		t.Fatal("qualification profile binding")
	}
	for _, mutate := range []func(*RelightQualificationPin){
		func(q *RelightQualificationPin) { q.RuntimeLockSHA = strings.Repeat("c", 64) },
		func(q *RelightQualificationPin) { q.Calibration.ArtifactID = uuid.New() },
		func(q *RelightQualificationPin) { q.Calibration.SHA256 = strings.Repeat("c", 64) },
		func(q *RelightQualificationPin) { q.Backend = "cuda" },
		func(q *RelightQualificationPin) { q.Report = q.Calibration },
		func(q *RelightQualificationPin) { q.IssuerID = uuid.Nil },
		func(q *RelightQualificationPin) { q.PolicySHA = "invalid" },
	} {
		bad := pin
		mutate(&bad)
		if got, err := CompilePinnedRelightTaskPlan(context.Background(), p.Base, raw, paramsBody, runtime, cost, bad); err == nil || got.Profile != nil {
			t.Fatal("foreign/invalid qualification accepted")
		}
	}
	plan, err := CompileRelightTaskPlan(context.Background(), p.Base, raw, paramsBody, runtime, cost)
	if err != nil || plan.Operation != "image.relight" || plan.ParamsSHA != paramsSHA || plan.ReservedBytes != cost.OutputBytes {
		t.Fatal("relight plan", err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(paramsBody, &fields) != nil {
		t.Fatal("params")
	}
	fields["unknown"] = json.RawMessage(`true`)
	unknown, _ := json.Marshal(fields)
	delete(fields, "unknown")
	delete(fields, "strength")
	missing, _ := json.Marshal(fields)
	for _, invalid := range [][]byte{unknown, missing, []byte(`{}`)} {
		if plan, err := CompileRelightTaskPlan(context.Background(), p.Base, raw, invalid, runtime, cost); err == nil || plan.Operation != "" {
			t.Fatal("invalid authored params accepted")
		}
	}
	foreign := p.Base
	foreign.AssetVersionID = uuid.New()
	if _, err := CompileRelightTaskPlan(context.Background(), foreign, raw, paramsBody, runtime, cost); err == nil {
		t.Fatal("foreign binding")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileRelightTaskPlan(cancelled, p.Base, raw, paramsBody, runtime, cost); err != context.Canceled {
		t.Fatal("cancel", err)
	}
	body, err := CompileRelightExecutionProfile(p, raw, runtime, cost)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Backend   string                `json:"backend"`
		Renderer  string                `json:"renderer_version"`
		Runtime   string                `json:"relight_runtime_sha"`
		Resources LocalResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(body, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Runtime != runtime || profile.Resources != cost {
		t.Fatal("worker profile mismatch", string(body))
	}
	for i := 0; i < 5; i++ {
		limit := cost
		switch i {
		case 0:
			limit.PixelCount--
		case 1:
			limit.RAMBytes--
		case 2:
			limit.ScratchBytes--
		case 3:
			limit.OutputBytes--
		case 4:
			limit.VRAMBytes = -1
		}
		if got, err := CompileRelightExecutionProfile(p, raw, runtime, limit); err == nil || got != nil {
			t.Fatal("insufficient capacity", i)
		}
	}
	limit := cost
	limit.RAMBytes = 9007199254740992
	if got, err := CompileRelightExecutionProfile(p, raw, runtime, limit); err == nil || got != nil {
		t.Fatal("unsafe integer")
	}
	for _, pin := range []string{"", strings.Repeat("A", 64)} {
		if got, err := CompileRelightExecutionProfile(p, raw, pin, cost); err == nil || got != nil {
			t.Fatal("invalid runtime")
		}
	}
	changed := append(append([]byte{}, raw...), ' ')
	// Even a matching digest cannot admit noncanonical WorkingImage bytes.
	sum = sha256.Sum256(changed)
	p.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	if got, err := CompileRelightExecutionProfile(p, changed, runtime, cost); err == nil || got != nil {
		t.Fatal("noncanonical source")
	}
	p.Base.Source.SHA256 = strings.Repeat("c", 64)
	if got, err := CompileRelightExecutionProfile(p, raw, runtime, cost); err == nil || got != nil {
		t.Fatal("foreign source")
	}
}
