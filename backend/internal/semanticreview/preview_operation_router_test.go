package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

type previewRouterReader struct{ calls int }

func (r *previewRouterReader) ReadPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (Preview, error) {
	r.calls++
	return Preview{}, nil
}
func TestPreviewRouterDoesNotFallBackAcrossOperations(t *testing.T) {
	exposure, removal := &previewRouterReader{}, &previewRouterReader{}
	kind := "remove_object"
	scopeErr := error(nil)
	router := &PreviewOperationRouter{Exposure: exposure, Removal: removal, Resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (string, error) {
		return kind, scopeErr
	}}
	call := func() error {
		_, err := router.ReadPreview(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{})
		return err
	}
	if err := call(); err != nil || removal.calls != 1 || exposure.calls != 0 {
		t.Fatal("wrong removal route", err)
	}
	kind = "local_exposure"
	if err := call(); err != nil || removal.calls != 1 || exposure.calls != 1 {
		t.Fatal("wrong exposure route", err)
	}
	kind = "remove_object"
	router.Removal = nil
	if err := call(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing removal fell back", err)
	}
	kind = "unsupported_operation"
	if err := call(); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown kind routed", err)
	}
	scopeErr = ErrConflict
	if err := call(); !errors.Is(err, ErrConflict) {
		t.Fatal("scope error bypassed", err)
	}
	if exposure.calls != 1 || removal.calls != 1 {
		t.Fatal("rejected dispatch called provider")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := router.ReadPreview(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBackgroundCleanupPreviewRouterUsesOnlyOwnProvider(t *testing.T) {
	background, exposure, removal := &previewRouterReader{}, &previewRouterReader{}, &previewRouterReader{}
	router := &PreviewOperationRouter{BackgroundCleanup: background, Exposure: exposure, Removal: removal,
		Resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (string, error) {
			return "background_cleanup", nil
		}}
	if _, err := router.ReadPreview(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{}); err != nil {
		t.Fatal(err)
	}
	router.BackgroundCleanup = nil
	if _, err := router.ReadPreview(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing background provider did not fail closed", err)
	}
	if background.calls != 1 || exposure.calls != 0 || removal.calls != 0 {
		t.Fatal("operation provider fallback")
	}
}

func TestPreviewRouterStopsWhenResolverCancelsRequest(t *testing.T) {
	for _, resolverErr := range []error{nil, ErrConflict} {
		ctx, cancel := context.WithCancel(context.Background())
		reader := &previewRouterReader{}
		router := &PreviewOperationRouter{BackgroundCleanup: reader,
			Resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (string, error) {
				cancel()
				return "background_cleanup", resolverErr
			}}
		_, err := router.ReadPreview(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{})
		cancel()
		if !errors.Is(err, context.Canceled) || reader.calls != 0 {
			t.Fatal("provider invoked after resolver cancellation", reader.calls, err)
		}
	}
}

func TestRetouchPreviewRouterUsesOnlyOwnProvider(t *testing.T) {
	retouch, background := &previewRouterReader{}, &previewRouterReader{}
	router := &PreviewOperationRouter{Retouch: retouch, BackgroundCleanup: background, Resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (string, error) {
		return "retouch_region", nil
	}}
	if _, err := router.ReadPreview(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{}); err != nil {
		t.Fatal(err)
	}
	router.Retouch = nil
	if _, err := router.ReadPreview(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, PreviewRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing retouch fell back", err)
	}
	if retouch.calls != 1 || background.calls != 0 {
		t.Fatal("cross-operation fallback")
	}
}

func TestRetouchPreviewRouterStopsAfterResolverFailureOrCancellation(t *testing.T) {
	for _, mode := range []string{"error", "cancel", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			retouch, other := &previewRouterReader{}, &previewRouterReader{}
			scopeErr := errors.New("scope resolution unavailable")
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, intent, target := uuid.New(), uuid.New(), uuid.New()
			request := PreviewRequest{ExpectedRevision: "1", TargetRevision: "2"}
			router := &PreviewOperationRouter{Retouch: retouch, Exposure: other, Removal: other, BackgroundCleanup: other,
				Resolve: func(_ context.Context, gotWho auth.Identity, gotTenant, gotIntent, gotTarget uuid.UUID, gotRequest PreviewRequest) (string, error) {
					if gotWho.UserID != who.UserID || gotWho.SessionID != who.SessionID || gotTenant != tenant || gotIntent != intent || gotTarget != target || gotRequest != request { t.Fatal("routing lost request scope") }
					if mode == "error" { return "retouch_region", scopeErr }
					if mode == "cancel" { cancel(); return "retouch_region", nil }
					return "unsupported", nil
				}}
			got, err := router.ReadPreview(ctx, who, tenant, intent, target, request)
			want := scopeErr
			if mode == "cancel" { want = context.Canceled }
			if mode == "unknown" { want = ErrInvalid }
			if !errors.Is(err, want) || got.SchemaVersion != 0 || retouch.calls != 0 || other.calls != 0 { t.Fatal("resolver refusal dispatched a provider", mode, err) }
		})
	}
}
