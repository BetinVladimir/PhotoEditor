package semanticreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These query/store recorders exercise production readers, not SQL authorization.
// The real SQL authority chain has separate integration scenarios.
type cleanupReadQuery struct {
	t             *testing.T
	docs          cleanupPhysicalDocuments
	task, result  uuid.UUID
	rows, queries int
	revoked       bool
}
type cleanupReadRow struct {
	values []any
	err    error
}

func (r cleanupReadRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("fixture column count")
	}
	for i, v := range r.values {
		target := reflect.ValueOf(dest[i]).Elem()
		value := reflect.ValueOf(v)
		if !value.Type().AssignableTo(target.Type()) {
			return fmt.Errorf("fixture column %d type %T", i, v)
		}
		target.Set(value)
	}
	return nil
}

type cleanupReadRows struct {
	pgx.Rows
	values [][]any
	index  int
}

func (r *cleanupReadRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *cleanupReadRows) Scan(dest ...any) error {
	return (cleanupReadRow{values: r.values[r.index-1]}).Scan(dest...)
}
func (r *cleanupReadRows) Close()     {}
func (r *cleanupReadRows) Err() error { return nil }
func (q *cleanupReadQuery) source(ref v2input.Ref, media string) PlanSource {
	return PlanSource{Ref: ref, Bucket: "fixture", Key: ref.ArtifactID.String(), Version: "immutable-v1", Bytes: int64(len(q.docs.bodies[ref.ArtifactID.String()])), MediaType: media}
}
func (q *cleanupReadQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.rows++
	if !strings.Contains(sql, "photo.semantic_background_cleanup_preview_presentation_source(") || len(args) != 6 {
		q.t.Fatal("unexpected presentation query", sql)
	}
	if q.revoked {
		return cleanupReadRow{err: pgx.ErrNoRows}
	}
	s := q.source(q.docs.presentationRef, "application/json")
	return cleanupReadRow{values: []any{q.task, q.result, 1, int64(1), s.Ref.ArtifactID, s.Ref.SHA256, s.Bucket, s.Key, s.Version, s.Bytes, s.MediaType}}
}
func (q *cleanupReadQuery) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.queries++
	if !strings.Contains(sql, "photo.semantic_background_cleanup_preview_presentation_sidecars(") || len(args) != 7 {
		q.t.Fatal("unexpected sidecars query", sql)
	}
	p := q.docs.presentation
	refs := []v2input.Ref{p.NumericalEvidence, p.WorkingImage, p.Pixels, p.Before, p.After, p.Affected, p.Protected}
	roles := []string{"preview_evidence", "working_image", "pixels", "before", "after", "affected", "protected"}
	out := &cleanupReadRows{}
	for i, ref := range refs {
		media := "application/json"
		if i == 2 {
			media = "application/x-npy"
		}
		if i >= 3 {
			media = "image/png"
		}
		s := q.source(ref, media)
		out.values = append(out.values, []any{roles[i], ref.ArtifactID, ref.SHA256, s.Bucket, s.Key, s.Version, s.Bytes, s.MediaType})
	}
	return out, nil
}

type cleanupReadStore struct {
	t              *testing.T
	query          *cleanupReadQuery
	opened, closed int
	mode           string
	cancel         context.CancelFunc
}
type cleanupReadStream struct {
	io.Reader
	store *cleanupReadStore
}

func (r *cleanupReadStream) Close() error { r.store.closed++; return nil }
func (s *cleanupReadStore) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.opened++
	body, ok := s.query.docs.bodies[o.Key]
	if !ok || o.VersionID != "immutable-v1" || o.Bytes != int64(len(body)) {
		s.t.Fatal("unbound object read", o)
	}
	sum := physicalFixtureSHA(body)
	if o.SHA256 != sum {
		s.t.Fatal("object hash changed before read", o)
	}
	p := s.query.docs.presentation
	if o.Key == p.Pixels.ArtifactID.String() && s.mode == "corrupt_pixels" || o.Key == p.After.ArtifactID.String() && s.mode == "corrupt_after" {
		body = append([]byte(nil), body...)
		body[len(body)-1] ^= 1
	}
	if o.Key == p.Protected.ArtifactID.String() {
		switch s.mode {
		case "changed_result":
			s.query.result = uuid.New()
		case "revoked":
			s.query.revoked = true
		case "cancel":
			s.cancel()
		}
	}
	return &cleanupReadStream{Reader: bytes.NewReader(body), store: s}, nil
}

func TestBackgroundCleanupPhysicalReaderCompletePipeline(t *testing.T) {
	for _, mode := range []string{"valid", "corrupt_pixels", "corrupt_after", "changed_result", "revoked", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			docs := buildBackgroundCleanupPhysicalDocuments(t)
			run := uuid.New()
			query := &cleanupReadQuery{t: t, docs: docs, task: uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+docs.params.TargetID.String())), result: uuid.New()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &cleanupReadStore{t: t, query: query, mode: mode, cancel: cancel}
			got, err := ReadPhysicalBackgroundCleanupPreview(ctx, query, store, "fixture", auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, run, docs.params, docs.source, docs.params.Plan.SHA256, docs.srgbSHA)
			if mode == "valid" {
				if err != nil || !got.verified || got.linkage.Document.TaskID != query.task || !bytes.Equal(got.image.ImageBytes, docs.bodies[docs.presentation.WorkingImage.ArtifactID.String()]) {
					t.Fatal("physical pipeline failed", err)
				}
				signed, checks := 0, 0
				request := PreviewRequest{ExpectedRevision: docs.params.WorkflowRevision, TargetRevision: docs.params.TargetRevision, PlanSHA: docs.params.Plan.SHA256, CandidateSHA: docs.params.Candidate.SHA256}
				response, err := BuildBackgroundCleanupPreviewResponse(ctx, got, request, time.Now(), func(_ context.Context, obj objectstore.Object, _ time.Time) (string, error) {
					signed++
					if obj.VersionID != "immutable-v1" || obj.SHA256 != physicalFixtureSHA(docs.bodies[obj.Key]) {
						t.Fatal("unbound signed media", obj)
					}
					return "https://media.example.test/" + obj.Key, nil
				}, func(context.Context) error { checks++; return nil })
				if err != nil || signed != 4 || checks != 2 || response.SchemaVersion != 1 || response.QualityStatus != "review" {
					t.Fatal("verified pipeline did not produce review response", err, signed, checks)
				}
				if query.rows < 4 || query.queries < 2 {
					t.Fatal("missing repeated authority reads", query.rows, query.queries)
				}
			} else if err == nil || got.verified {
				t.Fatal("invalid physical preview escaped", mode, err)
			}
			if store.opened != store.closed {
				t.Fatal("stream leaked", store.opened, store.closed)
			}
		})
	}
}

func physicalFixtureSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
