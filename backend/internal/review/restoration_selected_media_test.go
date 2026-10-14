package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

func TestSelectedRestorationMediaRechecksLiveDecision(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
		key := []byte(strings.Repeat("k", 32))
		body := "restored-image"
		image := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA([]byte(body))}
		sourceRaw := []byte("restoration-source")
		source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(sourceRaw)}
		grant := SelectedPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(), Kind: "restoration",
			DecisionHash: strings.Repeat("b", 64), Route: "candidate", Variant: "preview1600", Role: "image",
			Artifact: image.ID, SHA256: image.SHA256, ObjectKeySHA: previewObjectKeySHA("restoration/image"), ObjectVersion: "v1", ExpiresUnix: now.Add(time.Minute).Unix()}
		token, err := SealSelectedPreviewGrant(key, grant, now)
		if err != nil {
			t.Fatal(err)
		}
		reads := 0
		service := CandidatePreviewService{
			RestorationSelection: restorationSelectionReaderFunc(func(_ context.Context, who auth.Identity, tenant, review uuid.UUID) (RestorationResultSelection, error) {
				reads++
				if who.UserID != grant.User || who.SessionID != grant.Session || tenant != grant.Tenant || review != grant.Review {
					t.Fatal("restoration grant scope changed")
				}
				hash := grant.DecisionHash
				if revoke && reads == 2 {
					hash = strings.Repeat("c", 64)
				}
				return RestorationResultSelection{SchemaVersion: 1, ReviewID: review, DecisionHash: hash, Route: "candidate", Image: &source}, nil
			}),
			Catalog: previewCatalogFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
				if side != "after" || variant != "preview1600" {
					t.Fatal("wrong restoration side")
				}
				return CandidatePreviewObjects{Source: source, Derivative: image,
					DerivativeObject: objectstore.Object{Key: "restoration/image", VersionID: "v1", SHA256: image.SHA256, ContentType: "image/jpeg", Bytes: int64(len(body))}}, nil
			}),
			Source: previewSourceFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, _ string) (ArtifactRef, []byte, error) {
				return source, sourceRaw, nil
			}),
			Objects: serviceObjects{raw: body}, Clock: func() time.Time { return now },
		}
		data, mime, err := service.ReadSelectedRenderPreview(context.Background(), key, token, now)
		if revoke {
			if err == nil || len(data) != 0 || mime != "" {
				t.Fatal("revoked restoration still served", err)
			}
		} else if err != nil || string(data) != body || mime != "image/jpeg" || reads != 2 {
			t.Fatal("selected restoration refused", err, mime, reads)
		}
	}
}
