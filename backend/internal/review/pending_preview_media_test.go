package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
)

func TestIssuePendingPreviewRechecksCatalogueAfterCreatingLinks(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	raw := imageFixtures(t)[0]
	image, err := ParseWorkingImage(raw)
	if err != nil { t.Fatal(err) }
	source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(raw)}
	derivative := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	icc := strings.Repeat("b", 64)
	profile, err := media.ProfileHash("preview1600", icc)
	if err != nil { t.Fatal(err) }
	manifestBody, err := canonical.Marshal(RenderDerivativeManifest{Source: source, Derivative: derivative,
		Variant: "preview1600", ProfileHash: profile, Width: image.Canvas.Width, Height: image.Canvas.Height,
		MediaType: "image/jpeg", ColorProfile: "srgb", MetadataPolicy: "stripped", Alpha: false})
	if err != nil { t.Fatal(err) }
	manifest := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(manifestBody)}
	objects := CandidatePreviewObjects{Source: source, Manifest: manifest, Derivative: derivative, ProfileHash: profile,
		ManifestObject: objectstore.Object{Key: "manifest", VersionID: "v1", SHA256: manifest.SHA256, ContentType: "application/json", Bytes: int64(len(manifestBody))},
		DerivativeObject: objectstore.Object{Key: "image", VersionID: "v2", SHA256: derivative.SHA256, ContentType: "image/jpeg", Bytes: 100}}
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable", true: "changed"}[changed], func(t *testing.T) {
			lookups := 0
			service := CandidatePreviewService{SRGBSHA: icc, Clock: func() time.Time { return now },
				Catalog: previewCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (CandidatePreviewObjects, error) {
					lookups++
					current := objects
					if changed && lookups == 4 { current.DerivativeObject.VersionID = "v3" }
					return current, nil
				}),
				Source: previewSourceFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (ArtifactRef, []byte, error) {
					return source, append([]byte(nil), raw...), nil
				}),
				Objects: serviceObjects{raw: string(manifestBody)},
				Signer: previewSignerFunc(func(_ context.Context, object objectstore.Object, _ time.Duration) (objectstore.ReadURL, error) {
					return objectstore.ReadURL{URL: "https://media.test/object?versionId=" + object.VersionID, ExpiresAt: now.Add(5 * time.Minute)}, nil
				})}
			issued, err := service.IssuePendingPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
				uuid.New(), uuid.New(), "before", "preview1600", []byte(strings.Repeat("k", 32)), "https://api.test")
			if changed {
				if domain.AsError(err).Code != "STALE_CANDIDATE_REVIEW" || issued.Review != uuid.Nil { t.Fatal("stale pending links issued", err) }
			} else if err != nil || issued.Review == uuid.Nil || lookups != 4 {
				t.Fatal("stable pending preview unavailable", err, lookups)
			}
		})
	}
}

func TestPendingPreviewMediaRefusesDecisionAfterIssuance(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
		key := []byte(strings.Repeat("k", 32))
		sourceRaw, imageRaw := []byte("source-bytes"), "verified-image"
		source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(sourceRaw)}
		image := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA([]byte(imageRaw))}
		catalogue := CandidatePreviewObjects{Source: source, Derivative: image,
			DerivativeObject: objectstore.Object{Key: "preview/image", VersionID: "v1", SHA256: image.SHA256, ContentType: "image/jpeg", Bytes: int64(len(imageRaw))}}
		grant := PendingPreviewGrant{Version: 1, Tenant: uuid.New(), Review: uuid.New(), User: uuid.New(), Session: uuid.New(),
			Side: "after", Variant: "preview1600", Source: source.ID, SourceSHA: source.SHA256,
			Role: "image", Artifact: image.ID, SHA256: image.SHA256, ObjectKeySHA: previewObjectKeySHA("preview/image"), ObjectVersion: "v1", ExpiresUnix: now.Add(time.Minute).Unix()}
		token, err := SealPendingPreviewGrant(key, grant, now)
		if err != nil {
			t.Fatal(err)
		}
		lookups := 0
		service := CandidatePreviewService{
			Catalog: previewCatalogFunc(func(_ context.Context, who auth.Identity, tenant, review uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
				lookups++
				if who.UserID != grant.User || who.SessionID != grant.Session || tenant != grant.Tenant || review != grant.Review || side != grant.Side || variant != grant.Variant {
					t.Fatal("pending capability scope changed")
				}
				if revoked && lookups == 2 {
					return CandidatePreviewObjects{}, domain.NewError("NOT_FOUND", 404)
				}
				return catalogue, nil
			}),
			Source: previewSourceFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, _ string) (ArtifactRef, []byte, error) {
				return source, sourceRaw, nil
			}),
			Objects: serviceObjects{raw: imageRaw}, Clock: func() time.Time { return now },
		}
		body, mediaType, err := service.ReadPendingPreview(context.Background(), key, token, now)
		if revoked {
			if err == nil || len(body) != 0 || mediaType != "" {
				t.Fatal("decided/revoked preview returned bytes", err)
			}
		} else if err != nil || string(body) != imageRaw || mediaType != "image/jpeg" || lookups != 2 {
			t.Fatal("pending preview refused", err, mediaType, lookups)
		}
	}
}

func TestPendingPreviewGrantRejectsReassignedObjectKey(t *testing.T) {
	ref := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	grant := PendingPreviewGrant{Role: "image", Artifact: ref.ID, SHA256: ref.SHA256,
		ObjectKeySHA: previewObjectKeySHA("original/image"), ObjectVersion: "v1"}
	object := objectstore.Object{Key: "reassigned/image", VersionID: "v1",
		SHA256: ref.SHA256, ContentType: "image/jpeg", Bytes: 10}
	if pendingGrantObjectMatches(grant, ref, object, 100) {
		t.Fatal("same-version same-content reassigned object accepted")
	}
}
