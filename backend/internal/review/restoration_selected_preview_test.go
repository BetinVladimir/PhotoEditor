package review

import (
	"context"
	"math"
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

type restorationSelectionReaderFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (RestorationResultSelection, error)

func (f restorationSelectionReaderFunc) ReadRestorationResultSelection(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (RestorationResultSelection, error) {
	return f(ctx, who, tid, id)
}

func TestSelectedRestorationPreviewSkipsWithoutPhysicalLookup(t *testing.T) {
	tenant, reviewID := uuid.New(), uuid.New()
	reader := restorationSelectionReaderFunc(func(_ context.Context, _ auth.Identity, tid, id uuid.UUID) (RestorationResultSelection, error) {
		if tid != tenant || id != reviewID {
			t.Fatal("selection scope changed")
		}
		return RestorationResultSelection{SchemaVersion: 1, ReviewID: id, DecisionHash: strings.Repeat("a", 64), Route: "skip"}, nil
	})
	calls := 0
	catalog := previewCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, string) (CandidatePreviewObjects, error) {
		calls++
		return CandidatePreviewObjects{}, nil
	})
	service := CandidatePreviewService{RestorationSelection: reader, Catalog: catalog}
	_, err := service.SelectedRestorationPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, reviewID, "preview1600")
	if domain.AsError(err).Code != "NOT_FOUND" || calls != 0 {
		t.Fatal("skipped restoration reached preview", err, calls)
	}
}

func TestSelectedRestorationPreviewPinsRouteSourceAndDecision(t *testing.T) {
	for _, tc := range []struct {
		route, side string
		change      bool
	}{
		{"base", "before", false}, {"candidate", "after", false}, {"candidate", "after", true},
	} {
		t.Run(tc.route+map[bool]string{true: "_changed", false: "_valid"}[tc.change], func(t *testing.T) {
			raw := imageFixtures(t)[0]
			parsed, err := ParseWorkingImage(raw)
			if err != nil {
				t.Fatal(err)
			}
			selected := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(raw)}
			derivative := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			icc := strings.Repeat("b", 64)
			profile, err := media.ProfileHash("preview1600", icc)
			if err != nil {
				t.Fatal(err)
			}
			scale := math.Min(1, 1600/math.Max(float64(parsed.Canvas.Width), float64(parsed.Canvas.Height)))
			mime := "image/jpeg"
			if parsed.Alpha != nil {
				mime = "image/webp"
			}
			manifest := RenderDerivativeManifest{Source: selected, Derivative: derivative, Variant: "preview1600", ProfileHash: profile, Width: max(1, int(math.RoundToEven(float64(parsed.Canvas.Width)*scale))), Height: max(1, int(math.RoundToEven(float64(parsed.Canvas.Height)*scale))), MediaType: mime, ColorProfile: "srgb", MetadataPolicy: "stripped", Alpha: parsed.Alpha != nil}
			body, err := canonical.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			manifestSHA := derivativeManifestSHA(body)
			value := CandidatePreviewObjects{Source: selected, Manifest: ArtifactRef{ID: uuid.New(), SHA256: manifestSHA}, Derivative: derivative, ProfileHash: profile, ManifestObject: objectstore.Object{Key: "manifest", VersionID: "v1", SHA256: manifestSHA, ContentType: "application/json", Bytes: int64(len(body))}, DerivativeObject: objectstore.Object{Key: "image", VersionID: "v2", SHA256: derivative.SHA256, ContentType: mime, Bytes: 100}}
			tenant, reviewID := uuid.New(), uuid.New()
			reads, signs := 0, 0
			reader := restorationSelectionReaderFunc(func(_ context.Context, _ auth.Identity, tid, id uuid.UUID) (RestorationResultSelection, error) {
				if tid != tenant || id != reviewID {
					t.Fatal("selection scope changed")
				}
				reads++
				hash := strings.Repeat("c", 64)
				if tc.change && reads == 2 {
					hash = strings.Repeat("d", 64)
				}
				return RestorationResultSelection{SchemaVersion: 1, ReviewID: id, DecisionHash: hash, Route: tc.route, Image: &selected}, nil
			})
			catalog := previewCatalogFunc(func(_ context.Context, _ auth.Identity, tid, id uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
				if tid != tenant || id != reviewID || side != tc.side || variant != "preview1600" {
					t.Fatal("wrong selected preview lookup", side, variant)
				}
				return value, nil
			})
			source := previewSourceFunc(func(_ context.Context, _ auth.Identity, tid, id uuid.UUID, side string) (ArtifactRef, []byte, error) {
				if tid != tenant || id != reviewID || side != tc.side {
					t.Fatal("wrong selected source lookup", side)
				}
				return selected, append([]byte(nil), raw...), nil
			})
			now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			signer := previewSignerFunc(func(_ context.Context, object objectstore.Object, _ time.Duration) (objectstore.ReadURL, error) {
				signs++
				return objectstore.ReadURL{URL: "https://media.test/versioned?versionId=" + object.VersionID, ExpiresAt: now.Add(5 * time.Minute)}, nil
			})
			service := CandidatePreviewService{RestorationSelection: reader, Catalog: catalog, Source: source, Objects: serviceObjects{raw: string(body)}, Signer: signer, SRGBSHA: icc, Clock: func() time.Time { return now }}
			result, err := service.SelectedRestorationPreview(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, reviewID, "preview1600")
			if tc.change {
				if domain.AsError(err).Code != "STALE_CANDIDATE_REVIEW" || result.Review != uuid.Nil {
					t.Fatal("changed decision yielded preview", err)
				}
			} else if err != nil || result.Review != reviewID || result.Side != tc.side || result.Source != selected {
				t.Fatal("selected preview binding lost", err)
			}
			if reads != 2 || signs != 2 {
				t.Fatal("selection/signing not completed", reads, signs)
			}
		})
	}
}
