package review

import (
	"context"
	"errors"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestCandidatePreviewServiceRequiresInstalledAuthority(t *testing.T) {
	service := CandidatePreviewService{}
	result, err := service.Preview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), "after", "preview1600")
	if err == nil || result.Review != uuid.Nil {
		t.Fatal("uninstalled preview service accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Preview(ctx, auth.Identity{}, uuid.New(), uuid.New(), "before", "preview1600")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

type previewCatalogFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (CandidatePreviewObjects, error)

func (f previewCatalogFunc) Lookup(c context.Context, w auth.Identity, t, r uuid.UUID, s, v string) (CandidatePreviewObjects, error) {
	return f(c, w, t, r, s, v)
}

type previewSourceFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (ArtifactRef, []byte, error)

func (f previewSourceFunc) ReadCandidatePreviewSource(c context.Context, w auth.Identity, t, r uuid.UUID, s string) (ArtifactRef, []byte, error) {
	return f(c, w, t, r, s)
}

type previewSignerFunc func(context.Context, objectstore.Object, time.Duration) (objectstore.ReadURL, error)

func (f previewSignerFunc) SignRead(c context.Context, o objectstore.Object, d time.Duration) (objectstore.ReadURL, error) {
	return f(c, o, d)
}

func TestCandidatePreviewReadsPinnedManifestAndRepeatsAuthorities(t *testing.T) {
	for _, mode := range []string{"valid", "catalog_changed", "source_changed", "manifest_changed", "bad_link", "missing_version", "wrong_version", "expired", "cancel_sign", "source_oversized", "source_empty"} {
		t.Run(mode, func(t *testing.T) {
			raw := imageFixtures(t)[0]
			image, err := ParseWorkingImage(raw)
			if err != nil {
				t.Fatal(err)
			}
			source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(raw)}
			output := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			icc := strings.Repeat("b", 64)
			profile, err := media.ProfileHash("preview1600", icc)
			if err != nil {
				t.Fatal(err)
			}
			scale := math.Min(1, 1600/math.Max(float64(image.Canvas.Width), float64(image.Canvas.Height)))
			mime := "image/jpeg"
			if image.Alpha != nil {
				mime = "image/webp"
			}
			manifest := RenderDerivativeManifest{Source: source, Derivative: output, Variant: "preview1600", ProfileHash: profile, Width: max(1, int(math.RoundToEven(float64(image.Canvas.Width)*scale))), Height: max(1, int(math.RoundToEven(float64(image.Canvas.Height)*scale))), MediaType: mime, ColorProfile: "srgb", MetadataPolicy: "stripped", Alpha: image.Alpha != nil}
			body, err := canonical.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			value := CandidatePreviewObjects{Source: source, Manifest: ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(body)}, Derivative: output, ProfileHash: profile, ManifestObject: objectstore.Object{Key: "manifest", VersionID: "v1", SHA256: derivativeManifestSHA(body), ContentType: "application/json", Bytes: int64(len(body))}, DerivativeObject: objectstore.Object{Key: "image", VersionID: "v2", SHA256: output.SHA256, ContentType: mime, Bytes: 100}}
			catalogs, sources, signs := 0, 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			catalog := previewCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (CandidatePreviewObjects, error) {
				catalogs++
				current := value
				if mode == "catalog_changed" && catalogs > 1 {
					current.DerivativeObject.VersionID = "v3"
				}
				return current, nil
			})
			reader := previewSourceFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string) (ArtifactRef, []byte, error) {
				sources++
				if mode == "source_oversized" {
					return source, make([]byte, 1048577), nil
				}
				if mode == "source_empty" {
					return source, nil, nil
				}
				if mode == "source_changed" && sources > 1 {
					return source, []byte(`{}`), nil
				}
				return source, append([]byte(nil), raw...), nil
			})
			signer := previewSignerFunc(func(_ context.Context, object objectstore.Object, _ time.Duration) (objectstore.ReadURL, error) {
				signs++
				link := objectstore.ReadURL{URL: "https://media.test/versioned?versionId="+object.VersionID, ExpiresAt: now.Add(5 * time.Minute)}
				if mode == "bad_link" {
					link.URL = "javascript:alert(1)"
				}
				if mode == "missing_version" { link.URL = "https://media.test/versioned" }
				if mode == "wrong_version" { link.URL = "https://media.test/versioned?versionId=other" }
				if mode == "expired" {
					link.ExpiresAt = now
				}
				if mode == "cancel_sign" {
					cancel()
				}
				return link, nil
			})
			objects := serviceObjects{raw: string(body)}
			if mode == "manifest_changed" {
				objects.raw = `{}`
			}
			service := CandidatePreviewService{Catalog: catalog, Source: reader, Objects: objects, Signer: signer, SRGBSHA: icc, Clock: func() time.Time { return now }}
			id := uuid.New()
			result, err := service.Preview(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), id, "after", "preview1600")
			if mode == "valid" {
				if err != nil || result.Review != id || result.Source != source || result.Width != manifest.Width || catalogs != 2 || sources != 2 || signs != 2 {
					t.Fatal("valid pinned preview refused", err, catalogs, sources, signs)
				}
				return
			}
			if err == nil || result.Review != uuid.Nil {
				t.Fatal("changed preview authority accepted")
			}
			if (mode == "source_oversized" || mode == "source_empty") && signs != 0 {
				t.Fatal("invalid source reached URL signing")
			}
		})
	}
}
