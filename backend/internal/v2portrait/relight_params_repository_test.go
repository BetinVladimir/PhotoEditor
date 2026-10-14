package v2portrait

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestRelightParamsRepositoryRejectsUnpreparedAndMixedScopes(t *testing.T) {
	ctx := context.Background()
	coordinator := RelightBootstrapCoordinator{Bucket: "fixture"}
	if _, err := coordinator.PublishPreparedRelightParams(ctx, nil, RelightBootstrapResult{Intent: RelightIntent{ParamsJCS: "{}"}}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("content-only intent published", err)
	}
	intent := RelightIntent{OwnerID: uuid.New(), ParamsJCS: "{}", ParamsSHA: "digest"}
	request := RelightBootstrapRequest{Tenant: uuid.New(), Run: uuid.New()}
	prepared := RelightBootstrapResult{Intent: intent, prepared: &relightPreparedAuthority{request: request, intent: intent}}
	repository := relightParamsRepository{coordinator: coordinator, prepared: prepared}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: request.Run, Owner: intent.OwnerID, Purpose: "task_params"}
	for _, change := range []func(*artifactjson.Scope){
		func(s *artifactjson.Scope) { s.LocalPortrait = true },
		func(s *artifactjson.Scope) { s.DonorPortrait = true },
		func(s *artifactjson.Scope) { s.AssemblyLease = uuid.New() },
		func(s *artifactjson.Scope) { s.Run = uuid.New() },
		func(s *artifactjson.Scope) { s.Tenant = uuid.New() },
		func(s *artifactjson.Scope) { s.Owner = uuid.New() },
		func(s *artifactjson.Scope) { s.Purpose = "review_selection" },
	} {
		mixed := scope
		change(&mixed)
		if _, err := repository.Reserve(ctx, mixed, intent.ParamsSHA, 2); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("mixed scope reached DB", err)
		}
		if _, err := repository.Complete(ctx, mixed, artifactjson.Reservation{}, objectstore.Object{SHA256: intent.ParamsSHA, Bytes: 2}); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("mixed completion reached DB", err)
		}
	}
	for _, wrong := range []struct {
		sha  string
		size int64
	}{{"changed", 2}, {intent.ParamsSHA, 3}} {
		if _, err := repository.Reserve(ctx, scope, wrong.sha, wrong.size); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("changed content reached DB", err)
		}
	}
}
