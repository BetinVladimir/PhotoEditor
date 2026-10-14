package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

func TestSelectedRenderPreviewReadRechecksDecisionAndObject(t *testing.T) {
	for _, mode := range []string{"valid", "decision_revoked", "object_replaced", "object_key_replaced", "source_replaced", "wrong_session", "bad_bytes"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			key := []byte(strings.Repeat("k", 32))
			raw := "jpeg-image-bytes"
			sum := sha256.Sum256([]byte(raw))
			sha := hex.EncodeToString(sum[:])
			sourceRaw := []byte("source-document")
			source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(sourceRaw)}
			image := ArtifactRef{ID: uuid.New(), SHA256: sha}
			value := CandidatePreviewObjects{
				Source: source, Derivative: image,
				DerivativeObject: objectstore.Object{Key: "selected/image", VersionID: "v1", SHA256: sha, ContentType: "image/jpeg", Bytes: int64(len(raw))},
			}
			grant := SelectedPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(), Kind: "render",
				DecisionHash: strings.Repeat("b", 64), Route: "candidate", Variant: "preview1600", Role: "image",
				Artifact: image.ID, SHA256: sha, ObjectKeySHA: previewObjectKeySHA("selected/image"), ObjectVersion: "v1", ExpiresUnix: now.Add(2 * time.Minute).Unix()}
			token, err := SealSelectedPreviewGrant(key, grant, now)
			if err != nil {
				t.Fatal(err)
			}
			selectionCalls, catalogCalls := 0, 0
			reader := renderSelectionReaderFunc(func(_ context.Context, who auth.Identity, tenant, review uuid.UUID) (RenderResultSelection, error) {
				selectionCalls++
				if tenant != grant.Tenant || review != grant.Review || who.UserID != grant.User || who.SessionID != grant.Session {
					t.Fatal("grant scope not preserved")
				}
				if mode == "wrong_session" {
					return RenderResultSelection{}, objectstore.ErrInvalid
				}
				hash := grant.DecisionHash
				if mode == "decision_revoked" && selectionCalls == 2 {
					hash = strings.Repeat("c", 64)
				}
				return RenderResultSelection{SchemaVersion: 1, ReviewID: review, DecisionHash: hash, Route: "candidate", Image: &source}, nil
			})
			catalog := previewCatalogFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
				catalogCalls++
				if side != "after" || variant != "preview1600" {
					t.Fatal("wrong selected side or variant", side, variant)
				}
				out := value
				if mode == "object_replaced" && catalogCalls == 2 {
					out.DerivativeObject.VersionID = "v2"
				}
				if mode == "object_key_replaced" && catalogCalls == 2 {
					out.DerivativeObject.Key = "selected/reassigned"
				}
				return out, nil
			})
			body := raw
			if mode == "bad_bytes" {
				body = "altered-image-bytes"
			}
			sourceCalls := 0
			service := CandidatePreviewService{Selection: reader, Catalog: catalog,
				Source: previewSourceFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, _ string) (ArtifactRef, []byte, error) {
					sourceCalls++
					if mode == "source_replaced" && sourceCalls == 2 { return source, []byte("changed-source"), nil }
					return source, sourceRaw, nil
				}),
				Objects: serviceObjects{raw: body}, Clock: func() time.Time { return now }}
			bytes, media, err := service.ReadSelectedRenderPreview(context.Background(), key, token, now)
			if mode == "valid" {
				if err != nil || string(bytes) != raw || media != "image/jpeg" || selectionCalls != 2 || catalogCalls != 2 || sourceCalls != 2 {
					t.Fatal("valid read failed", err, media, selectionCalls, catalogCalls, sourceCalls)
				}
			} else if err == nil || len(bytes) != 0 || media != "" {
				t.Fatal("revoked or substituted bytes escaped", mode, err)
			}
		})
	}
}
