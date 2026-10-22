package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestBackgroundCleanupPreviewResponseRejectsUnverifiedBeforeSigning(t *testing.T) {
	called := false
	sign := func(context.Context, objectstore.Object, time.Time) (string, error) { called = true; return "", nil }
	_, err := BuildBackgroundCleanupPreviewResponse(context.Background(), PhysicalBackgroundCleanupPreview{}, PreviewRequest{}, time.Now(), sign,
		func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("unverified preview reached signing")
	}
}

// This fixture tests response composition after physical verification. It does
// not perform image verification, qualification, receipt sealing or SQL ACL IO.
func backgroundCleanupResponseFixture() (PhysicalBackgroundCleanupPreview, PreviewRequest) {
	p := backgroundCleanupParamsFixture()
	physical := PhysicalBackgroundCleanupPreview{verified: true}
	physical.linkage.Presentation = BackgroundCleanupPreviewPresentation{Params: p, Width: 100, Height: 80,
		WorkingImage: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}}
	physical.linkage.Document.Source.Ref = v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}
	for i := 3; i < 7; i++ {
		physical.linkage.Sidecars[i] = PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256},
			Key: "fixture/" + uuid.NewString(), Version: "immutable-v1", Bytes: 256, MediaType: "image/png"}
	}
	return physical, PreviewRequest{ExpectedRevision: p.WorkflowRevision, TargetRevision: p.TargetRevision, PlanSHA: p.Plan.SHA256, CandidateSHA: p.Candidate.SHA256}
}

func TestBackgroundCleanupResponseSignsExactFourVersionPinnedObjects(t *testing.T) {
	physical, request := backgroundCleanupResponseFixture()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	signed, checks := 0, 0
	sign := func(_ context.Context, obj objectstore.Object, expiry time.Time) (string, error) {
		source := physical.linkage.Sidecars[signed+3]
		if obj.Key != source.Key || obj.VersionID != source.Version || obj.SHA256 != source.Ref.SHA256 || obj.Bytes != source.Bytes || obj.ContentType != "image/png" || !expiry.Equal(now.Add(time.Minute)) {
			t.Fatal("signer received different object identity", obj, source)
		}
		signed++
		return "https://media.example.test/" + source.Ref.ArtifactID.String(), nil
	}
	got, err := BuildBackgroundCleanupPreviewResponse(context.Background(), physical, request, now, sign, func(context.Context) error { checks++; return nil })
	if err != nil || signed != 4 || checks != 2 || got.QualityStatus != "review" || len(got.Limitations) == 0 {
		t.Fatal("response did not preserve photographer review", got, err, signed, checks)
	}
	p := physical.linkage.Presentation.Params
	if err := got.Validate(p.TenantID, p.IntentID, p.TargetID, request, now); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundCleanupResponseDiscardsURLsOnRevocationOrSignerFailure(t *testing.T) {
	for _, mode := range []string{"revoked_before", "revoked_after", "signer", "cancel_after"} {
		t.Run(mode, func(t *testing.T) {
			physical, request := backgroundCleanupResponseFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("fixture failure")
			signed, checks := 0, 0
			sign := func(context.Context, objectstore.Object, time.Time) (string, error) {
				signed++
				if mode == "signer" && signed == 2 {
					return "", failure
				}
				return "https://media.example.test/object", nil
			}
			authority := func(context.Context) error {
				checks++
				if (mode == "revoked_before" && checks == 1) || (mode == "revoked_after" && checks == 2) {
					return failure
				}
				if mode == "cancel_after" && checks == 2 {
					cancel()
				}
				return nil
			}
			got, err := BuildBackgroundCleanupPreviewResponse(ctx, physical, request, time.Now(), sign, authority)
			expected := failure
			if mode == "cancel_after" {
				expected = context.Canceled
			}
			if err != expected || got.SchemaVersion != 0 || got.Before.URL != "" || got.After.URL != "" {
				t.Fatal("partial signed response escaped", got, err)
			}
			if mode == "revoked_before" && signed != 0 {
				t.Fatal("revoked request signed media")
			}
		})
	}
}

func TestBackgroundCleanupResponseRejectsStaleRequestBeforeSigning(t *testing.T) {
	for _, field := range []string{"workflow_revision", "target_revision", "plan_sha", "candidate_sha"} {
		t.Run(field, func(t *testing.T) {
			physical, request := backgroundCleanupResponseFixture()
			switch field {
			case "workflow_revision":
				request.ExpectedRevision = "999"
			case "target_revision":
				request.TargetRevision = "999"
			case "plan_sha":
				request.PlanSHA = alteredResponseSHA(request.PlanSHA)
			case "candidate_sha":
				request.CandidateSHA = alteredResponseSHA(request.CandidateSHA)
			}
			called := false
			got, err := BuildBackgroundCleanupPreviewResponse(context.Background(), physical, request, time.Now(),
				func(context.Context, objectstore.Object, time.Time) (string, error) { called = true; return "", nil },
				func(context.Context) error { called = true; return nil })
			if err != ErrConflict || called || got.SchemaVersion != 0 {
				t.Fatal("stale request reached signing", got, err)
			}
		})
	}
}

func TestBackgroundCleanupResponseCanceledBeforeIO(t *testing.T) {
	physical, request := backgroundCleanupResponseFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	got, err := BuildBackgroundCleanupPreviewResponse(ctx, physical, request, time.Now(),
		func(context.Context, objectstore.Object, time.Time) (string, error) { called = true; return "", nil },
		func(context.Context) error { called = true; return nil })
	if err != context.Canceled || called || got.SchemaVersion != 0 {
		t.Fatal("canceled response performed IO", got, err)
	}
}

func alteredResponseSHA(value string) string {
	if value[0] == '0' {
		return "1" + value[1:]
	}
	return "0" + value[1:]
}

func TestBackgroundCleanupResponseCancellationDuringEachMediaSignature(t *testing.T) {
	for _, stop := range []int{1, 2, 3, 4} {
		physical, request := backgroundCleanupResponseFixture()
		ctx, cancel := context.WithCancel(context.Background())
		signed, checks := 0, 0
		got, err := BuildBackgroundCleanupPreviewResponse(ctx, physical, request, time.Now(),
			func(context.Context, objectstore.Object, time.Time) (string, error) {
				signed++
				if signed == stop {
					cancel()
				}
				return "https://media.example.test/object", nil
			}, func(context.Context) error { checks++; return nil })
		cancel()
		if err != context.Canceled || signed != stop || got.SchemaVersion != 0 || got.Before.URL != "" || got.After.URL != "" || got.Affected.URL != "" || got.Protected.URL != "" {
			t.Fatal("cancellation leaked partial response or continued signing", stop, signed, checks, got, err)
		}
	}
}

func TestBackgroundCleanupResponsePreservesCancellationWhenSignerAlsoFails(t *testing.T) {
	for _, stop := range []int{1, 2, 3, 4} {
		physical, request := backgroundCleanupResponseFixture()
		ctx, cancel := context.WithCancel(context.Background())
		signed, checks := 0, 0
		got, err := BuildBackgroundCleanupPreviewResponse(ctx, physical, request, time.Now(),
			func(context.Context, objectstore.Object, time.Time) (string, error) {
				signed++
				if signed == stop {
					cancel()
					return "", errors.New("signer failed after cancellation")
				}
				return "https://media.example.test/object", nil
			}, func(context.Context) error { checks++; return nil })
		cancel()
		if !errors.Is(err, context.Canceled) || signed != stop || checks != 1 || got.SchemaVersion != 0 ||
			got.Before.URL != "" || got.After.URL != "" || got.Affected.URL != "" || got.Protected.URL != "" {
			t.Fatal("cancelled signer returned wrong outcome or continued IO", stop, signed, checks, got, err)
		}
	}
}
