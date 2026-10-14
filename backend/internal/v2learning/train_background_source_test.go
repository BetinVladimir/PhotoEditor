package v2learning

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type trainingBackgroundFreezerFixture struct {
	freeze func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.Ref, string, string, *string) ([]byte, error)
}

func (f trainingBackgroundFreezerFixture) Freeze(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, style v2input.Ref, id, icc string, assume *string) ([]byte, error) {
	return f.freeze(ctx, who, tenant, project, style, id, icc, assume)
}
func TestTrainingBackgroundSourceFreezesExactAuthorizedStyleAsset(t *testing.T) {
	for _, mode := range []string{"valid", "keep", "unknown_mode", "portable", "style", "icc", "cancelled", "missing_resolver"} {
		t.Run(mode, func(t *testing.T) {
			start := TrainingStart{TenantID: uuid.New(), ProjectID: uuid.New(), ActorID: uuid.New(), AuthSessionID: uuid.New()}
			who := auth.Identity{UserID: start.ActorID, SessionID: start.AuthSessionID}
			body, err := canonical.Bytes([]byte(`{"extensions":{"background":{"mode":"asset","asset_id":"installed"}}}`))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "keep" {
				body = []byte(`{}`)
			}
			if mode == "unknown_mode" {
				body = []byte(`{"extensions":{"background":{"mode":"unrecognized"}}}`)
			}
			style := v2input.Ref{ArtifactID: uuid.New(), SHA256: trainingRegionDigest(body)}
			icc := strings.Repeat("c", 64)
			store := trainingRecipeStore{objects: map[uuid.UUID]objectstore.Object{style.ArtifactID: {Key: "style", VersionID: "v1", SHA256: style.SHA256, Bytes: int64(len(body)), ContentType: "application/json"}}, bodies: map[string][]byte{"style": body}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			freezer := trainingBackgroundFreezerFixture{freeze: func(ctx context.Context, actor auth.Identity, tenant, project uuid.UUID, ref v2input.Ref, id, work string, assume *string) ([]byte, error) {
				calls++
				if actor != who || tenant != start.TenantID || project != start.ProjectID || ref != style || id != "installed" || work != icc || assume != nil {
					t.Fatal("unscoped asset resolution")
				}
				pin := v2draft.FrozenBackgroundAsset{SchemaVersion: 1, PortableAssetID: id, StyleVersionID: uuid.New(), Style: ref, Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, ExpectedBytes: 10, MediaType: "image/png", Suffix: ".png", WorkICCSHA: work}
				switch mode {
				case "portable":
					pin.PortableAssetID = "other"
				case "style":
					pin.Style.ArtifactID = uuid.New()
				case "icc":
					pin.WorkICCSHA = strings.Repeat("d", 64)
				case "cancelled":
					cancel()
				}
				return canonical.Marshal(pin)
			}}
			source := TrainingBackgroundSource{Catalog: store, Objects: store, Assets: freezer}
			if mode == "missing_resolver" {
				source.Assets = nil
			}
			got, err := source.LoadBackground(ctx, who, start, style, icc)
			valid := mode == "valid" || mode == "keep"
			if (err == nil) != valid {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "valid" && (got == nil || got.Style != style || calls != 1) {
				t.Fatal("background pin missing")
			}
			if mode != "valid" && got != nil {
				t.Fatal("unexpected pin")
			}
			if (mode == "keep" || mode == "unknown_mode" || mode == "missing_resolver") && calls != 0 {
				t.Fatal("unnecessary asset lookup")
			}
		})
	}
}
