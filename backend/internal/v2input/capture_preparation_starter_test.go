package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
	"time"
)

type captureStartStore struct {
	config                     CaptureDraftConfig
	freeze, begin, load, stage int
}

func (s *captureStartStore) FreezeSnapshot(context.Context, CaptureAssessmentLease, SessionVersionBinding) (uuid.UUID, error) {
	s.freeze++
	return uuid.New(), nil
}
func (s *captureStartStore) BeginDraft(_ context.Context, l CaptureAssessmentLease, b SessionVersionBinding, lock string, profile json.RawMessage, _ int64) (CaptureDraftConfig, error) {
	s.begin++
	s.config.ProjectID = l.ProjectID
	s.config.Capture.JobID = l.JobID
	var policy CapturePolicy
	if json.Unmarshal(l.Policy, &policy) != nil {
		return CaptureDraftConfig{}, ErrInvalid
	}
	s.config.Capture.Params = CapturePrepareParams{1, b, policy.Calibration, lock}
	body, _ := canonical.Marshal(s.config.Capture.Params)
	s.config.Capture.ParamsJCS = string(body)
	s.config.Capture.ParamsSHA, _ = canonical.Hash(s.config.Capture.Params)
	s.config.Capture.Profile = profile
	return s.config, nil
}
func (s *captureStartStore) LoadDraft(context.Context, CaptureAssessmentLease) (CaptureDraftConfig, error) {
	s.load++
	return s.config, nil
}
func (s *captureStartStore) StagePreparation(_ context.Context, _ CaptureAssessmentLease, ref Ref, digest string, profile json.RawMessage) error {
	if ref.SHA256 != s.config.Capture.ParamsSHA || !digestPattern.MatchString(digest) || string(profile) != string(s.config.Capture.Profile) {
		return ErrInvalid
	}
	s.stage++
	return nil
}

type captureStartSource struct {
	source CaptureSourceImage
	inputs CaptureImageInputs
}

func (s captureStartSource) LoadInputs(context.Context, CaptureAssessmentLease) (CaptureSourceImage, CaptureImageInputs, error) {
	return s.source, s.inputs, nil
}

type captureStartCommitter struct {
	calls     int
	store     *captureStartStore
	transport *memoryPublisher
}

func (c *captureStartCommitter) CommitCapturePreparation(_ context.Context, l CaptureAssessmentLease, run, workflow uuid.UUID, task GraphTask, params json.RawMessage) error {
	expectedRun, expectedWorkflow, _, _ := CaptureDraftIDs(l.JobID)
	expectedTask, _ := TaskID(workflow, "capture.prepare", 0)
	if run != expectedRun || workflow != expectedWorkflow || task.ID != expectedTask || task.Built.Count != 4 || len(c.transport.bodies) != 2 ||
		string(params) != c.store.config.Capture.ParamsJCS || c.store.stage != c.calls+1 {
		return ErrInvalid
	}
	c.calls++
	return nil
}
func TestCapturePreparationResumesExactFrozenDraft(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	binding := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	l := CaptureAssessmentLease{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), ReceiptID: uuid.New(), SessionID: uuid.New(), AssetVersionID: binding.AssetVersionID, Until: time.Now().Add(time.Minute), CreatedAt: time.Now(), Attempts: 1}
	policy := CapturePolicy{1, []string{"focus"}, 30, ref()}
	l.Policy, _ = canonical.Marshal(policy)
	source := CaptureSourceImage{Binding: binding, Image: review.WorkingImage{Canvas: review.Canvas{Width: 6000, Height: 4000}, TransformChain: json.RawMessage(`{"steps":[]}`)}}
	store := &captureStartStore{}
	transport := &memoryPublisher{bodies: map[uuid.UUID][]byte{}}
	commit := &captureStartCommitter{store: store, transport: transport}
	docs := &stableAnalysisDocuments{refs: map[string]artifactjson.Ref{}}
	service := CapturePreparationService{Store: store, Source: captureStartSource{source, CaptureImageInputs{Pixels: ref()}}, Documents: docs, Committer: commit,
		TransportForRun: func(CaptureAssessmentLease, uuid.UUID) Publisher { return transport }, Backend: "cuda", ModelLockSHA: strings.Repeat("c", 64), RAMLimit: 32 << 30, VRAMLimit: 12 << 30, ScratchLimit: 32 << 30, ReservedBytes: 64 << 20}
	if err := service.StartCapturePreparation(context.Background(), l, nil); err != nil {
		t.Fatal(err)
	}
	run, workflow, _, _ := CaptureDraftIDs(l.JobID)
	l.PreparationWorkflowID = &workflow
	// A changed installation must not rewrite the profile or model lock of a
	// pending draft whose parameter publication previously completed.
	service.ModelLockSHA = strings.Repeat("d", 64)
	service.Backend = "cpu"
	if err := service.StartCapturePreparation(context.Background(), l, &CapturePreparationProgress{workflow, run, "draft"}); err != nil {
		t.Fatal(err)
	}
	if store.freeze != 1 || store.begin != 1 || store.load != 1 || store.stage != 2 || commit.calls != 2 || store.config.Capture.Params.ModelLockSHA != strings.Repeat("c", 64) {
		t.Fatalf("recovery drift: %+v", store)
	}
}
