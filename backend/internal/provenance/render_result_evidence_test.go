package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type signingRenderAdoptionFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (v2input.Ref, error)

func (f signingRenderAdoptionFunc) ReadSigningAdoptedRenderRoot(ctx context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask) (v2input.Ref, error) {
	return f(ctx, w, r, t)
}

func TestSigningRenderResultRequiresStableAdoptionAndPhysicalBytes(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_root", "revoked", "changed_adoption", "changed_bytes", "hash", "invalid", "cancelled", "session"} {
		t.Run(mode, func(t *testing.T) {
			image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			raw, root := contractBytes(t, v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: image, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, ChangedMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}})
			if mode == "invalid" {
				raw = []byte(`{}`)
				root.SHA256 = bodyDigest(raw)
			}
			outputs, err := canonical.Marshal([]v2input.Ref{root})
			if err != nil {
				t.Fatal(err)
			}
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}
			if mode == "session" {
				who.SessionID = uuid.Nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			adoptions, reads := 0, 0
			denied := errors.New("adoption revoked")
			adoption := signingRenderAdoptionFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (v2input.Ref, error) {
				adoptions++
				value := root
				if mode == "foreign_root" || mode == "changed_adoption" && adoptions == 2 {
					value.ArtifactID = uuid.New()
				}
				if mode == "revoked" && adoptions == 2 {
					return v2input.Ref{}, denied
				}
				return value, nil
			})
			documents := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, ref v2input.Ref, maximum int) ([]byte, error) {
				reads++
				if ref != root || maximum != 1<<20 {
					t.Fatal("document scope changed")
				}
				if mode == "hash" || mode == "changed_bytes" && reads == 2 {
					return []byte(`{}`), nil
				}
				return raw, nil
			})
			actual, result, err := ReadSigningRenderResult(ctx, adoption, documents, who, ready, task)
			if mode == "valid" {
				if err != nil || actual != root || result.WorkingImage != image || reads != 2 || adoptions != 2 {
					t.Fatal("valid adopted root rejected", err)
				}
				return
			}
			if err == nil || actual.ArtifactID != uuid.Nil {
				t.Fatal("invalid adopted root accepted")
			}
			if mode == "revoked" && !errors.Is(err, denied) {
				t.Fatal("revocation cause lost", err)
			}
			if (mode == "cancelled" || mode == "session") && (reads != 0 || adoptions != 0) {
				t.Fatal("invalid scope performed IO")
			}
		})
	}
}
