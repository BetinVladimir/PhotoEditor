package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type signingRenderProducerFunc func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (CanonicalSigningTask, error)

func (f signingRenderProducerFunc) ReadSigningRenderImageProducer(c context.Context, w auth.Identity, r ReadySigningExport, p v2input.Ref) (CanonicalSigningTask, error) {
	return f(c, w, r, p)
}
func TestSigningRenderImageProducerBindsPhysicalChildAndRepeatsProducer(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_image", "changed_producer", "changed_adoption"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			image := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
			raw, root := contractBytes(t, v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: image, ChangedMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}})
			outputs, _ := canonical.Marshal([]v2input.Ref{root})
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: sha, ResultDigest: sha, Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			calls, adoptions := 0, 0
			catalogue := signingRenderProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (CanonicalSigningTask, error) {
				calls++
				current := task
				if mode == "changed_producer" && calls == 2 {
					current.Result = uuid.New()
				}
				return current, nil
			})
			adoption := signingRenderAdoptionFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (v2input.Ref, error) {
				adoptions++
				current := root
				if mode == "changed_adoption" && adoptions > 1 {
					current.ArtifactID = uuid.New()
				}
				return current, nil
			})
			documents := signingDocumentsFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) ([]byte, error) {
				return raw, nil
			})
			requested := image
			if mode == "foreign_image" {
				requested.ArtifactID = uuid.New()
			}
			producer, err := (BoundSigningRenderImageProducer{Catalog: catalogue, Adoption: adoption, Documents: documents}).ResolveSigningImageProducer(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New()}, requested)
			if mode == "valid" {
				if err != nil || producer != task.Task || calls != 2 {
					t.Fatal("valid child producer refused", err)
				}
				return
			}
			if err == nil || producer != uuid.Nil {
				t.Fatal("invalid child producer accepted")
			}
		})
	}
}
func TestSigningImageProducerFallbackOnlyOnAbsentDirectOutput(t *testing.T) {
	denied := errors.New("authority denied")
	for _, cause := range []error{nil, denied, pgx.ErrNoRows} {
		fallback := 0
		id := uuid.New()
		direct := signingProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error) {
			if cause != nil {
				return uuid.Nil, cause
			}
			return id, nil
		})
		render := signingProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error) {
			fallback++
			return id, nil
		})
		_, err := (SigningImageProducerWithRender{Direct: direct, Render: render}).ResolveSigningImageProducer(context.Background(), auth.Identity{}, ReadySigningExport{}, v2input.Ref{})
		if cause == pgx.ErrNoRows {
			if fallback != 1 || err != nil {
				t.Fatal("absent output did not resolve render child")
			}
		} else if fallback != 0 || !errors.Is(err, cause) {
			t.Fatal("fallback hid direct authority outcome")
		}
	}
}
