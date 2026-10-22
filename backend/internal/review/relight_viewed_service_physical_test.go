package review

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

type viewedLiveCatalogue struct {
	lookup   previewCatalogFunc
	snapshot func() (CandidateSnapshot, error)
}

func (s viewedLiveCatalogue) Lookup(ctx context.Context, who auth.Identity, tenant, id uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
	return s.lookup(ctx, who, tenant, id, side, variant)
}
func (s viewedLiveCatalogue) ReadRelightViewedSnapshot(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (CandidateSnapshot, error) {
	return s.snapshot()
}

type viewedVersionedObjects struct {
	read func(objectstore.Object) ([]byte, error)
}

func (s viewedVersionedObjects) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	raw, err := s.read(o)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

// Catalogue and SQL authority are explicit adapters. Actual service code reads
// and hashes real JPEG bytes through the pinned versioned Objects interface.
// This is not a real S3 or PostgreSQL integration fixture.
func TestRelightViewedPhysicalPairIssuanceAndChanges(t *testing.T) {
	for _, mode := range []string{"stable", "changed_catalogue", "changed_snapshot", "corrupt_bytes", "expired_during_io", "cancel_during_io"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
			clock := now
			key := []byte(strings.Repeat("k", 32))
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, id := uuid.New(), uuid.New()
			sourceRaw := imageFixtures(t)[0]
			working, err := ParseWorkingImage(sourceRaw)
			if err != nil {
				t.Fatal(err)
			}
			catalog := map[string]CandidatePreviewObjects{}
			payloads := map[string][]byte{}
			tokens := map[string]string{}
			for index, side := range []string{"before", "after"} {
				canvas := image.NewRGBA(image.Rect(0, 0, int(working.Canvas.Width), int(working.Canvas.Height)))
				canvas.SetRGBA(0, 0, color.RGBA{R: uint8(40 + index*80), A: 255})
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 88}); err != nil {
					t.Fatal(err)
				}
				raw := encoded.Bytes()
				source := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(sourceRaw)}
				output := ArtifactRef{ID: uuid.New(), SHA256: derivativeManifestSHA(raw)}
				object := objectstore.Object{Key: side + "/preview.jpg", VersionID: "original-version", SHA256: output.SHA256, ContentType: "image/jpeg", Bytes: int64(len(raw))}
				catalog[side] = CandidatePreviewObjects{Source: source, Derivative: output, DerivativeObject: object}
				payloads[object.Key] = append([]byte(nil), raw...)
				grant := PendingPreviewGrant{Version: 1, Tenant: tenant, Review: id, User: who.UserID, Session: who.SessionID, Side: side, Variant: "preview1600", Source: source.ID, SourceSHA: source.SHA256, Role: "image", Artifact: output.ID, SHA256: output.SHA256, ObjectKeySHA: previewObjectKeySHA(object.Key), ObjectVersion: object.VersionID, ExpiresUnix: now.Add(time.Minute).Unix()}
				token, err := SealPendingPreviewGrant(key, grant, now)
				if err != nil {
					t.Fatal(err)
				}
				tokens[side] = token
			}
			snapshot := CandidateSnapshot{TenantID: tenant, ReviewID: id, RunID: uuid.New(), Revision: 1, State: "pending", Kind: "image.relight", Base: catalog["before"].Source, Candidate: catalog["after"].Source}
			lookups, snapshots, reads := 0, 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service := CandidatePreviewService{Clock: func() time.Time { return clock }, Catalog: viewedLiveCatalogue{
				lookup: func(_ context.Context, actor auth.Identity, tid, rid uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
					lookups++
					if actor != who || tid != tenant || rid != id || variant != "preview1600" {
						t.Fatal("principal/source scope lost")
					}
					current := catalog[side]
					if mode == "changed_catalogue" && lookups == 6 {
						current.DerivativeObject.VersionID = "replacement-version"
					}
					return current, nil
				},
				snapshot: func() (CandidateSnapshot, error) {
					snapshots++
					current := snapshot
					if mode == "changed_snapshot" && snapshots == 2 {
						current.Revision++
					}
					return current, nil
				},
			}, Source: previewSourceFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, side string) (ArtifactRef, []byte, error) {
				return catalog[side].Source, append([]byte(nil), sourceRaw...), nil
			}),
				Objects: viewedVersionedObjects{read: func(object objectstore.Object) ([]byte, error) {
					reads++
					if object.VersionID != "original-version" {
						t.Fatal("newer version substituted")
					}
					raw := append([]byte(nil), payloads[object.Key]...)
					if mode == "corrupt_bytes" {
						raw[0] ^= 1
					}
					if mode == "expired_during_io" && reads == 2 {
						clock = now.Add(time.Minute)
					}
					if mode == "cancel_during_io" {
						cancel()
					}
					return raw, nil
				}}}
			receipt, err := service.AcknowledgeRelightViewed(ctx, who, tenant, id, tokens["before"], tokens["after"], key)
			if mode != "stable" {
				if err == nil || receipt.Token != "" {
					t.Fatal("changed pair issued acknowledgement", mode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			grant, err := OpenRelightViewedGrant(key, receipt.Token, now)
			if err != nil {
				t.Fatal(err)
			}
			if !MatchesRelightViewedGrant(grant, tenant, id, who.UserID, who.SessionID, 1, snapshot.Base, snapshot.Candidate, now) || reads != 2 || lookups != 6 || snapshots != 2 {
				t.Fatal("verified pair or boundary checks lost", reads, lookups, snapshots)
			}
		})
	}
}
