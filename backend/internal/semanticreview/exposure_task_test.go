package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type exposureTransportPublisher struct {
	calls int
	bad   bool
}

type forbiddenExposureObjects struct{ calls int }

func (o *forbiddenExposureObjects) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	o.calls++
	return objectstore.Object{}, ErrInvalid
}
func (o *forbiddenExposureObjects) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	o.calls++
	return objectstore.Object{}, ErrInvalid
}

func (p *exposureTransportPublisher) Publish(_ context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	if p.bad {
		sha = strings.Repeat("0", 64)
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(fmt.Sprintf("%s:%d", contract, ordinal))), SHA256: sha}, nil
}

func TestExposureTaskProfileInventoryAndPublication(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	ev := .4
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: uuid.New(), Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}, Intent: ref(), Candidate: ref(), Mask: ref(), Transform: ref(), Kind: "local_exposure", Strength: .8, EV: &ev, PreservePersons: true}
	child := uuid.New()
	task := uuid.NewSHA1(child, []byte("semantic.edit:"+edit.TargetID.String()))
	body, _ := canonical.Marshal(edit)
	sha, _ := canonical.Hash(edit)
	draft := ApprovedTaskDraft{TaskID: task, TargetID: edit.TargetID, Params: v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: sha}, Body: body, Kind: edit.Kind}
	deps := v2input.ExposureDependencies{Reference: ref(), Source: v2input.CaptureImageInputs{Pixels: ref()}, ReferenceImage: v2input.CaptureImageInputs{Pixels: ref()}, MaskPixels: ref(), Protection: ref(), ProtectionPixels: ref()}
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu"}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	frozen, err := CompileExposureTask(child, draft, deps, 24, 20, owner, limits, pin)
	if err != nil {
		t.Fatal(err)
	}
	objects := &forbiddenExposureObjects{}
	candidate := edit.Candidate.ArtifactID
	locked := LockedTarget{ID: edit.TargetID, AssetVersionID: edit.Source.AssetVersionID, CandidateArtifactID: &candidate, State: "proposed", Revision: 1}
	tenant, intent, actor, session, parent := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	scoped, err := NewApprovedExposurePublisher(&pgxpool.Pool{}, objects, "fixture", tenant, intent, actor, session, parent, child, 1, locked, frozen)
	if err != nil {
		t.Fatal("scoped publisher", err)
	}
	for _, args := range []struct {
		task     uuid.UUID
		ordinal  int
		contract string
	}{
		{uuid.New(), 0, "InputClosureChunk"}, {task, -1, "InputClosureChunk"}, {task, v2input.MaxChunks, "InputClosureChunk"},
		{task, 0, "InputClosureManifest"}, {task, v2input.MaxChunks + 1, "InputClosureManifest"}, {task, 0, "Params"},
	} {
		if _, err := scoped.Publish(context.Background(), args.task, args.ordinal, args.contract, []byte("{}")); err == nil {
			t.Fatal("invalid publication accepted")
		}
	}
	if objects.calls != 0 {
		t.Fatal("invalid scope reached object IO")
	}
	for _, mutate := range []func(*LockedTarget){
		func(t *LockedTarget) { t.State = "queued" }, func(t *LockedTarget) { id := uuid.New(); t.BatchID = &id },
		func(t *LockedTarget) { t.AssetVersionID = uuid.New() }, func(t *LockedTarget) { t.CandidateArtifactID = nil },
	} {
		bad := locked
		mutate(&bad)
		if _, err := NewApprovedExposurePublisher(&pgxpool.Pool{}, objects, "fixture", tenant, intent, actor, session, parent, child, 1, bad, frozen); err == nil {
			t.Fatal("substituted target publisher")
		}
	}
	node, err := frozen.DraftTask()
	if err != nil || node.Operation != "semantic.local_exposure" || node.ResourceClass != "cpu" || node.ID != task {
		t.Fatal("draft", err)
	}
	var profile ExposureExecutionProfile
	if json.Unmarshal(node.ExecutionProfile, &profile) != nil || profile.Qualification != pin || profile.ResourceEstimate.PixelCount != 480 || profile.ResourceEstimate.VRAMBytes != 0 {
		t.Fatal("profile binding")
	}
	node.ExecutionProfile[0] = '!'
	owned, _ := frozen.DraftTask()
	if owned.ExecutionProfile[0] != '{' {
		t.Fatal("mutable profile")
	}
	draft.Body[0] = '!'
	publisher := &exposureTransportPublisher{}
	prepared, err := PublishExposureTask(context.Background(), frozen, publisher)
	if err != nil || prepared.Graph.ID != task || prepared.Graph.Built.Count != 12 || len(prepared.Graph.Direct) != 12 || publisher.calls != 2 || prepared.ParamsJCS[0] != '{' {
		t.Fatal("publication", err)
	}
	// Caller-supplied cursors are never invoked during independent verification.
	prepared.Graph.Open = func(context.Context) (v2input.Cursor, error) {
		t.Fatal("untrusted cursor invoked")
		return nil, ErrInvalid
	}
	verified, err := VerifyExposurePublication(context.Background(), frozen, prepared)
	if err != nil || verified.Graph.Open == nil || verified.Graph.Built.SHA256 != prepared.Graph.Built.SHA256 {
		t.Fatal("independent publication verification", err)
	}
	for index, alter := range []func(*PreparedExposureTask){
		func(p *PreparedExposureTask) { p.Graph.ID = uuid.New() },
		func(p *PreparedExposureTask) { p.Graph.Built.SHA256 = strings.Repeat("c", 64) },
		func(p *PreparedExposureTask) { p.Graph.Built.Count-- },
		func(p *PreparedExposureTask) { p.Graph.Built.Chunks[0].SHA256 = strings.Repeat("c", 64) },
		func(p *PreparedExposureTask) { p.Graph.Built.Manifest.SHA256 = strings.Repeat("c", 64) },
		func(p *PreparedExposureTask) { p.Graph.Built.Manifest.ArtifactID = p.Graph.Built.Chunks[0].ArtifactID },
		func(p *PreparedExposureTask) { p.Graph.Built.Chunks[0].ArtifactID = p.Graph.Direct[0].ArtifactID },
		func(p *PreparedExposureTask) { p.Graph.Direct[0].SHA256 = strings.Repeat("c", 64) },
		func(p *PreparedExposureTask) { p.Draft.ExecutionProfile[0] = '!' },
		func(p *PreparedExposureTask) { p.ParamsJCS[0] = '!' },
	} {
		changed := prepared
		changed.Graph.Built.Chunks = append([]v2input.Ref(nil), prepared.Graph.Built.Chunks...)
		changed.Graph.Direct = append([]v2input.Ref(nil), prepared.Graph.Direct...)
		changed.Draft.ExecutionProfile = append(json.RawMessage(nil), prepared.Draft.ExecutionProfile...)
		changed.ParamsJCS = append(json.RawMessage(nil), prepared.ParamsJCS...)
		alter(&changed)
		if _, err := VerifyExposurePublication(context.Background(), frozen, changed); err == nil {
			t.Fatalf("publication substitution %d accepted", index)
		}
	}
	if _, err := PublishExposureTask(context.Background(), frozen, &exposureTransportPublisher{bad: true}); err == nil {
		t.Fatal("wrong transport SHA accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	none := &exposureTransportPublisher{}
	if _, err := PublishExposureTask(ctx, frozen, none); err == nil || none.calls != 0 {
		t.Fatal("cancelled publication")
	}
	draft.Body = append([]byte(nil), prepared.ParamsJCS...)
	for _, alter := range []func(*ApprovedTaskDraft, *ExposureQualificationPin, *ExposureResourceEstimate){
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			d.TargetID = uuid.New()
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			d.TaskID = uuid.New()
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			d.Kind = "remove_object"
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			p.Backend = "cuda"
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			p.Report = p.Calibration
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			p.RuntimeLockSHA = "bad"
		},
		func(d *ApprovedTaskDraft, p *ExposureQualificationPin, l *ExposureResourceEstimate) {
			l.RAMBytes = profile.ResourceEstimate.RAMBytes - 1
		},
	} {
		d, p, l := draft, pin, limits
		alter(&d, &p, &l)
		if _, err := CompileExposureTask(child, d, deps, 24, 20, owner, l, p); err == nil {
			t.Fatal("invalid task/profile accepted")
		}
	}
}
