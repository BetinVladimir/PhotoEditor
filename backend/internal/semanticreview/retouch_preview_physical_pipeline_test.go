package semanticreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These query/store recorders exercise production readers, not SQL authorization.
// The real SQL authority chain has separate integration scenarios.
type retouchPipelineReadQuery struct {
	t             *testing.T
	docs          retouchPipelinePhysicalDocuments
	task, result  uuid.UUID
	rows, queries int
	adapter       v2input.Ref
	revoked       bool
	receiptHook   func()
	receiptError  error
}
type retouchPipelineReadRow struct {
	values []any
	err    error
}

func (r retouchPipelineReadRow) Scan(dest ...any) error {
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

type retouchPipelineReadRows struct {
	pgx.Rows
	values [][]any
	index  int
}

func (r *retouchPipelineReadRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *retouchPipelineReadRows) Scan(dest ...any) error {
	return (retouchPipelineReadRow{values: r.values[r.index-1]}).Scan(dest...)
}
func (r *retouchPipelineReadRows) Close()     {}
func (r *retouchPipelineReadRows) Err() error { return nil }
func (q *retouchPipelineReadQuery) source(ref v2input.Ref, media string) PlanSource {
	return PlanSource{Ref: ref, Bucket: "fixture", Key: ref.ArtifactID.String(), Version: "immutable-v1", Bytes: int64(len(q.docs.bodies[ref.ArtifactID.String()])), MediaType: media}
}
func (q *retouchPipelineReadQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.rows++
	if strings.Contains(sql, "photo.seal_semantic_retouch_preview_receipt(") {
		if q.receiptHook != nil { q.receiptHook() }
		if q.receiptError != nil { return retouchPipelineReadRow{err: q.receiptError} }
		if len(args) != 15 || args[6] != q.adapter.ArtifactID || args[7] != q.adapter.SHA256 || args[8] != q.task || args[9] != q.result || args[10] != 1 || args[11] != int64(1) {
			q.t.Fatal("receipt generation scope lost", args)
		}
		_, digest, err := FreezeRetouchPreviewParams(q.docs.params)
		if err != nil {
			q.t.Fatal(err)
		}
		presentation := q.docs.presentation
		raw, err := canonical.Marshal(RetouchPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: q.task, ResultID: q.result, Attempt: 1, Fence: 1,
			Presentation: q.docs.presentationRef, WorkingImage: presentation.WorkingImage, Pixels: presentation.Pixels, NumericalEvidence: presentation.NumericalEvidence})
		if err != nil {
			q.t.Fatal(err)
		}
		return retouchPipelineReadRow{values: []any{raw}}
	}
	if !strings.Contains(sql, "photo.semantic_retouch_preview_presentation_source(") || len(args) != 8 {
		q.t.Fatal("unexpected presentation query", sql)
	}
	if args[6] != q.adapter.ArtifactID || args[7] != q.adapter.SHA256 {
		q.t.Fatal("adapter scope lost")
	}
	if q.revoked {
		return retouchPipelineReadRow{err: pgx.ErrNoRows}
	}
	s := q.source(q.docs.presentationRef, "application/json")
	return retouchPipelineReadRow{values: []any{q.task, q.result, 1, int64(1), s.Ref.ArtifactID, s.Ref.SHA256, s.Bucket, s.Key, s.Version, s.Bytes, s.MediaType}}
}
func (q *retouchPipelineReadQuery) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.queries++
	if !strings.Contains(sql, "photo.semantic_retouch_preview_presentation_sidecars(") || len(args) != 9 {
		q.t.Fatal("unexpected sidecars query", sql)
	}
	if args[6] != q.adapter.ArtifactID || args[7] != q.adapter.SHA256 {
		q.t.Fatal("adapter scope lost")
	}
	p := q.docs.presentation
	refs := []v2input.Ref{p.NumericalEvidence, p.WorkingImage, p.Pixels, p.Before, p.After, p.Affected, p.Protected}
	roles := []string{"preview_evidence", "working_image", "pixels", "before", "after", "affected", "protected"}
	out := &retouchPipelineReadRows{}
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

type retouchPipelineReadStore struct {
	t              *testing.T
	query          *retouchPipelineReadQuery
	opened, closed int
	mode           string
	cancel         context.CancelFunc
}
type retouchPipelineReadStream struct {
	io.Reader
	store *retouchPipelineReadStore
	readFailure, closeFailure bool
}

func (r *retouchPipelineReadStream) Read(p []byte) (int, error) {
	if r.readFailure { return 0, errors.New("physical fixture read failure") }
	return r.Reader.Read(p)
}
func (r *retouchPipelineReadStream) Close() error {
	r.store.closed++
	if r.closeFailure { return errors.New("physical fixture close failure") }
	return nil
}
func (s *retouchPipelineReadStore) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.opened++
	body, ok := s.query.docs.bodies[o.Key]
	if !ok || o.VersionID != "immutable-v1" || o.Bytes != int64(len(body)) {
		s.t.Fatal("unbound object read", o)
	}
	sum := retouchPhysicalFixtureSHA(body)
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
	role := ""
	for name, ref := range map[string]v2input.Ref{"pixels": p.Pixels, "before": p.Before, "after": p.After, "affected": p.Affected, "protected": p.Protected} {
		if o.Key == ref.ArtifactID.String() { role = name }
	}
	if role != "" && s.mode == "cancel_"+role { s.cancel() }
	return &retouchPipelineReadStream{Reader: bytes.NewReader(body), store: s,
		readFailure: role != "" && s.mode == "read_"+role,
		closeFailure: role != "" && s.mode == "close_"+role}, nil
}

func TestRetouchPhysicalReaderCompletePipeline(t *testing.T) {
	for _, mode := range []string{"valid", "corrupt_pixels", "corrupt_after", "changed_result", "revoked", "cancel", "read_pixels", "close_pixels", "read_before", "close_before", "read_after", "close_after", "read_affected", "close_affected", "read_protected", "close_protected", "cancel_pixels", "cancel_before", "cancel_after", "cancel_affected", "cancel_protected"} {
		t.Run(mode, func(t *testing.T) {
			docs := buildRetouchPhysicalDocuments(t)
			run := uuid.New()
			query := &retouchPipelineReadQuery{t: t, docs: docs, task: uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+docs.params.TargetID.String())), result: uuid.New(), adapter: v2input.Ref{ArtifactID: uuid.New(), SHA256: docs.params.Plan.SHA256}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &retouchPipelineReadStore{t: t, query: query, mode: mode, cancel: cancel}
			got, err := ReadPhysicalRetouchPreview(ctx, query, store, "fixture", auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, run, docs.params, query.adapter, docs.source, docs.params.Plan.SHA256, docs.srgbSHA)
			if mode == "valid" {
				if err != nil || !got.verified || got.linkage.Document.TaskID != query.task || !bytes.Equal(got.image.ImageBytes, docs.bodies[docs.presentation.WorkingImage.ArtifactID.String()]) {
					t.Fatal("physical pipeline failed", err)
				}
				receipt, err := SealRetouchPreviewReceipt(ctx, query, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, run, got)
				if err != nil || receipt.ResultID != query.result || receipt.Presentation != docs.presentationRef {
					t.Fatal("physical receipt linkage failed", err)
				}
				signed, checks := 0, 0
				request := PreviewRequest{ExpectedRevision: docs.params.WorkflowRevision, TargetRevision: docs.params.TargetRevision, PlanSHA: docs.params.Plan.SHA256, CandidateSHA: docs.params.Candidate.SHA256}
				response, err := BuildRetouchPreviewResponse(ctx, got, request, time.Now(), func(_ context.Context, obj objectstore.Object, _ time.Time) (string, error) {
					signed++
					if obj.VersionID != "immutable-v1" || obj.SHA256 != retouchPhysicalFixtureSHA(docs.bodies[obj.Key]) {
						t.Fatal("unbound signed media", obj)
					}
					return "https://media.example.test/" + obj.Key + "?versionId=" + obj.VersionID, nil
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
			if strings.HasPrefix(mode, "cancel_") && err != context.Canceled { t.Fatal("physical cancellation replaced", mode, err) }
			if store.opened != store.closed {
				t.Fatal("stream leaked", store.opened, store.closed)
			}
		})
	}
}

func retouchPhysicalFixtureSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}


func TestRetouchPhysicalReceiptStopsOnDatabaseFailureOrLateCancellation(t *testing.T) {
	for _, mode := range []string{"database_failure", "late_cancel", "cancel_and_failure"} {
		t.Run(mode, func(t *testing.T) {
			docs := buildRetouchPhysicalDocuments(t)
			run := uuid.New()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			query := &retouchPipelineReadQuery{t: t, docs: docs, task: uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+docs.params.TargetID.String())), result: uuid.New(), adapter: v2input.Ref{ArtifactID: uuid.New(), SHA256: docs.params.Plan.SHA256}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &retouchPipelineReadStore{t: t, query: query, mode: "valid", cancel: cancel}
			physical, err := ReadPhysicalRetouchPreview(ctx, query, store, "fixture", who, run, docs.params, query.adapter, docs.source, docs.params.Plan.SHA256, docs.srgbSHA)
			if err != nil || !physical.verified || store.opened != store.closed { t.Fatal("physical fixture failed", err) }
			failure := errors.New("receipt database unavailable")
			want := failure
			if mode != "late_cancel" { query.receiptError = failure }
			if mode != "database_failure" { query.receiptHook = cancel; want = context.Canceled }
			before := query.rows
			got, err := SealRetouchPreviewReceipt(ctx, query, who, run, physical)
			if !errors.Is(err, want) || got != (RetouchPreviewReceipt{}) || query.rows != before+1 {
				t.Fatal("receipt escaped failed or cancelled sealing", got, err, query.rows-before)
			}
		})
	}
}
