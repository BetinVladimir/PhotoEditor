package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRelightPublicationBoundaries(t *testing.T) {
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
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)} }
	p := RelightParams{SchemaVersion: 1, Base: binding, Depth: newRef(), Subject: newRef(), Protected: newRef(), Calibration: newRef(), Lights: []RelightLight{{Radius: .5}}}
	depth := review.RelativeDepth{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Float32Shape: []int64{image.Canvas.Height, image.Canvas.Width}, Canvas: image.Canvas, TransformChain: image.TransformChain, Normalization: "minmax_valid"}
	depthRaw, _ := canonical.Marshal(depth)
	subject := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: 1}
	subjectRaw, _ := canonical.Marshal(subject)
	guard := subject
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	guard.Selector = "manual"
	guardRaw, _ := canonical.Marshal(guard)
	digest := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	p.Depth.SHA256 = digest(depthRaw)
	p.Subject.SHA256 = digest(subjectRaw)
	p.Protected.SHA256 = digest(guardRaw)
	documents := RelightInputDocuments{Source: raw, Depth: depthRaw, Subject: subjectRaw, Protected: guardRaw}
	params, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	cost, _ := LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
	_, pin, reportRaw, calibrationRaw, expected := relightQualificationFixture(t)
	p.Calibration = pin.Calibration
	params, _, err = p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	evidence := RelightQualificationEvidence{Report: reportRaw, Calibration: calibrationRaw}
	workflow := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	intent, err := CompileRelightIntent(context.Background(), workflow, 0, who, binding, documents, params, pin.RuntimeLockSHA, cost, pin, evidence, expected)
	if err != nil || intent.OwnerID != uuid.NewSHA1(workflow, []byte("image.relight:0")) || intent.ParamsJCS != string(params) {
		t.Fatal("frozen intent", err)
	}
	checkRelightDepthReader(t, intent, documents)
	tx := &relightQualificationTx{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", ready: true}}
	if err = SaveRelightIntent(context.Background(), tx, uuid.New(), uuid.New(), intent); err != nil || tx.queries[1] != "SELECT photo.save_v28_relight_intent($1,$2,$3::jsonb)" {
		t.Fatal("intent SQL gate", err)
	}
	for _, ordinal := range []int{-1, 5000} {
		if _, err := CompileRelightIntent(context.Background(), workflow, ordinal, who, binding, documents, params, pin.RuntimeLockSHA, cost, pin, evidence, expected); err == nil {
			t.Fatal("ordinal bound")
		}
	}

	scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", AssemblyLease: uuid.New()}
	for _, mode := range []string{"good", "wrong", "error", "mutate", "cancel"} {
		ctx, cancel := context.WithCancel(context.Background())
		publisher := &localPlanPublisher{mode: mode}
		if mode == "cancel" {
			publisher.cancel = cancel
		}
		got, err := CompileAndPublishRelightTaskPlan(ctx, publisher, scope, binding, documents, params, strings.Repeat("a", 64), cost, pin, evidence, expected)
		cancel()
		if publisher.calls != 1 || publisher.scope != scope || !bytes.Equal(publisher.body, params) {
			t.Fatal("scope/bytes", mode)
		}
		if mode == "good" || mode == "mutate" {
			if err != nil || got.ParamsRef.ArtifactID == uuid.Nil || !bytes.Equal(got.Plan.Params, params) {
				t.Fatal("plan", mode, err)
			}
			if len(got.Inputs) != 11 {
				t.Fatal("published input inventory", len(got.Inputs))
			}
			hasParams := false
			for i, entry := range got.Inputs {
				if entry.Contract == "Params" {
					hasParams = entry.Ref == got.ParamsRef
				}
				if i > 0 && got.Inputs[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
					t.Fatal("input order")
				}
			}
			if !hasParams {
				t.Fatal("missing published Params")
			}
			var profile struct {
				Pin RelightQualificationPin `json:"relight_qualification"`
			}
			if json.Unmarshal(got.Plan.Profile, &profile) != nil || profile.Pin != pin {
				t.Fatal("published plan lost qualification")
			}
		} else if err == nil || got.ParamsRef != (v2input.Ref{}) || got.Plan.Params != nil {
			t.Fatal("partial result", mode)
		}
	}
	for _, input := range []v2input.Ref{binding.Source, p.Depth, p.Subject, p.Protected, p.Calibration, pin.Report, {ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, {ArtifactID: depth.Artifact.ID, SHA256: depth.Artifact.SHA256}, {ArtifactID: subject.Artifact.ID, SHA256: subject.Artifact.SHA256}, {ArtifactID: guard.Artifact.ID, SHA256: guard.Artifact.SHA256}} {
		publisher := &localPlanPublisher{mode: "alias", source: input.ArtifactID}
		if got, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, scope, binding, documents, params, strings.Repeat("a", 64), cost, pin, evidence, expected); err == nil || got.Plan.Params != nil {
			t.Fatal("input alias")
		}
	}
	for _, mutate := range []func(*artifactjson.Scope){
		func(s *artifactjson.Scope) { s.LocalPortrait = true }, func(s *artifactjson.Scope) { s.DonorPortrait = true },
		func(s *artifactjson.Scope) { s.AssemblyLease = uuid.Nil }, func(s *artifactjson.Scope) { s.Tenant = uuid.Nil },
		func(s *artifactjson.Scope) { s.Run = uuid.Nil }, func(s *artifactjson.Scope) { s.Owner = uuid.Nil }, func(s *artifactjson.Scope) { s.Purpose = "v2_input_manifest" },
	} {
		invalid := scope
		mutate(&invalid)
		publisher := &localPlanPublisher{}
		if _, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, invalid, binding, documents, params, strings.Repeat("a", 64), cost, pin, evidence, expected); err == nil || publisher.calls != 0 {
			t.Fatal("invalid scope IO")
		}
	}
	for _, mutate := range []func(*RelightQualificationPin){
		func(q *RelightQualificationPin) { q.RuntimeLockSHA = strings.Repeat("c", 64) },
		func(q *RelightQualificationPin) { q.Calibration.ArtifactID = uuid.New() },
		func(q *RelightQualificationPin) { q.PolicyID = uuid.Nil },
	} {
		bad := pin
		mutate(&bad)
		publisher := &localPlanPublisher{}
		got, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, scope, binding, documents, params, strings.Repeat("a", 64), cost, bad, evidence, expected)
		if err == nil || publisher.calls != 0 || got.Plan.Profile != nil {
			t.Fatal("invalid qualification performed IO")
		}
	}
	for _, mutate := range []func(*RelightInputDocuments){
		func(d *RelightInputDocuments) { d.Depth = nil }, func(d *RelightInputDocuments) { d.Subject = d.Protected },
		func(d *RelightInputDocuments) { d.Protected = nil }, func(d *RelightInputDocuments) { d.Source = nil },
	} {
		invalid := documents
		mutate(&invalid)
		publisher := &localPlanPublisher{}
		got, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, scope, binding, invalid, params, strings.Repeat("a", 64), cost, pin, evidence, expected)
		if err == nil || publisher.calls != 0 || got.Inputs != nil {
			t.Fatal("invalid documents performed IO")
		}
	}
	for _, mutate := range []func(*RelightQualificationEvidence){
		func(e *RelightQualificationEvidence) { e.Report = nil },
		func(e *RelightQualificationEvidence) { e.Calibration = nil },
		func(e *RelightQualificationEvidence) { e.Report = append(append([]byte{}, e.Report...), ' ') },
		func(e *RelightQualificationEvidence) { e.Calibration = append(append([]byte{}, e.Calibration...), ' ') },
	} {
		invalid := evidence
		mutate(&invalid)
		publisher := &localPlanPublisher{}
		got, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, scope, binding, documents, params, strings.Repeat("a", 64), cost, pin, invalid, expected)
		if err == nil || publisher.calls != 0 || got.Inputs != nil {
			t.Fatal("invalid evidence performed IO")
		}
	}
	denied := expected
	denied.LicenseCleared = false
	deniedPublisher := &localPlanPublisher{}
	if _, err := CompileAndPublishRelightTaskPlan(context.Background(), deniedPublisher, scope, binding, documents, params, strings.Repeat("a", 64), cost, pin, evidence, denied); err == nil || deniedPublisher.calls != 0 {
		t.Fatal("untrusted policy performed IO")
	}
	publisher := &localPlanPublisher{}
	if _, err := CompileAndPublishRelightTaskPlan(context.Background(), publisher, scope, binding, documents, []byte(`{}`), strings.Repeat("a", 64), cost, pin, evidence, expected); err == nil || publisher.calls != 0 {
		t.Fatal("invalid params IO")
	}
}
