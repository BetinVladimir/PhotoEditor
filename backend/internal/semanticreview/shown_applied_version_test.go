package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func TestShownVersionRejectsMissingAuthorityBeforeIO(t *testing.T) {
	if _, err := MaterializeShownAppliedVersion(context.Background(), nil, auth.Identity{}, uuid.New(), uuid.New(), []byte(`{}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}
func TestShownVersionHonorsCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MaterializeShownAppliedVersion(ctx, nil, auth.Identity{}, uuid.New(), uuid.New(), []byte(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

type shownVersionQuery struct {
	target, child, result, version uuid.UUID
	sha                            string
	operation                      string
	body                           []byte
	calls                          int
	cancel                         context.CancelFunc
 cancelAfterMutation context.CancelFunc
}
type shownVersionRow struct {
	q     *shownVersionQuery
	query string
	args  []any
}

func (q *shownVersionQuery) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.calls++
	return shownVersionRow{q, query, args}
}
func (r shownVersionRow) Scan(dest ...any) error {
	operation := r.q.operation
	if operation == "" {
		operation = "semantic.adopt_shown_exposure"
	}
	task := uuid.NewSHA1(r.q.child, []byte(operation+":"+r.q.target.String()))
	if strings.Contains(r.query, "semantic_shown_applied_image") {
		*dest[0].(*uuid.UUID) = r.q.child
		*dest[1].(*uuid.UUID) = task
		*dest[2].(*uuid.UUID) = r.q.result
		*dest[3].(*uuid.UUID) = uuid.New()
		*dest[4].(*string) = r.q.sha
		if r.q.cancel != nil {
			r.q.cancel()
		}
		return nil
	}
	if !strings.Contains(r.query, "materialize_semantic_shown_version") || r.args[4].(string) != string(r.q.body) {
		panic("unexpected materialization")
	}
	value := r.q.version
	*dest[0].(**uuid.UUID) = &value
 if r.q.cancelAfterMutation != nil { r.q.cancelAfterMutation() }
	return nil
}
func TestShownVersionExactDocumentAndDeterministicIdentity(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	raw := []byte(images[0])
	sum := sha256.Sum256(raw)
	q := &shownVersionQuery{target: uuid.New(), child: uuid.New(), result: uuid.New(), sha: hex.EncodeToString(sum[:]), body: raw}
	task := uuid.NewSHA1(q.child, []byte("semantic.adopt_shown_exposure:"+q.target.String()))
	q.version = uuid.NewSHA1(task, []byte("V27.ShownAppliedVersion"))
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	actual, err := MaterializeShownAppliedVersion(context.Background(), q, who, tenant, q.target, raw)
	if err != nil || actual != q.version || q.calls != 2 {
		t.Fatalf("version=%v calls=%d error=%v", actual, q.calls, err)
	}
	q.version = uuid.New()
	if _, err := MaterializeShownAppliedVersion(context.Background(), q, who, tenant, q.target, raw); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign version accepted: %v", err)
	}
	q.calls = 0
	q.sha = strings.Repeat("b", 64)
	if _, err := MaterializeShownAppliedVersion(context.Background(), q, who, tenant, q.target, raw); !errors.Is(err, ErrInvalid) || q.calls != 1 {
		t.Fatalf("SHA mismatch reached mutation: calls=%d error=%v", q.calls, err)
	}
}

type missingShownProjection struct{}
type missingShownProjectionRow struct{}

func (missingShownProjection) QueryRow(context.Context, string, ...any) pgx.Row {
	return missingShownProjectionRow{}
}
func (missingShownProjectionRow) Scan(...any) error { return pgx.ErrNoRows }
func TestShownAppliedImageMissingProjectionIsStateConflict(t *testing.T) {
	_, err := LoadShownAppliedImage(context.Background(), missingShownProjection{}, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
}
func TestShownDocumentMissingProjectionDoesNotReadStorage(t *testing.T) {
	_, err := ReadShownAppliedDocument(context.Background(), missingShownProjection{}, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), "photos", func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unexpected object read"); return nil, nil })
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestShownRemovalVersionUsesExactCopyTaskIdentity(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	raw := []byte(images[0])
	sum := sha256.Sum256(raw)
	q := &shownVersionQuery{target: uuid.New(), child: uuid.New(), result: uuid.New(), sha: hex.EncodeToString(sum[:]), body: raw, operation: "semantic.adopt_shown_removal"}
	task := uuid.NewSHA1(q.child, []byte(q.operation+":"+q.target.String()))
	q.version = uuid.NewSHA1(task, []byte("V27.ShownAppliedVersion"))
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	got, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw)
	if err != nil || got != q.version || q.calls != 2 {
		t.Fatalf("version=%v calls=%d error=%v", got, q.calls, err)
	}
	q.operation = "semantic.remove_object"
	q.calls = 0
	if _, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw); !errors.Is(err, ErrInvalid) || q.calls != 1 {
		t.Fatalf("non-copy producer reached mutation: calls=%d error=%v", q.calls, err)
	}
}

func TestShownBackgroundCleanupVersionUsesExactCopyTaskIdentity(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	raw := []byte(images[0])
	sum := sha256.Sum256(raw)
	q := &shownVersionQuery{target: uuid.New(), child: uuid.New(), result: uuid.New(), sha: hex.EncodeToString(sum[:]), body: raw, operation: "semantic.adopt_shown_background_cleanup"}
	task := uuid.NewSHA1(q.child, []byte(q.operation+":"+q.target.String()))
	q.version = uuid.NewSHA1(task, []byte("V27.ShownAppliedVersion"))
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	got, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw)
	if err != nil || got != q.version || q.calls != 2 {
		t.Fatalf("version=%v calls=%d error=%v", got, q.calls, err)
	}
	q.operation = "semantic.background_cleanup"
	q.calls = 0
	if _, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw); !errors.Is(err, ErrInvalid) || q.calls != 1 {
		t.Fatalf("non-copy producer reached mutation: calls=%d error=%v", q.calls, err)
	}
}

func TestShownRetouchVersionUsesExactCopyTaskIdentity(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	raw := []byte(images[0])
	sum := sha256.Sum256(raw)
	q := &shownVersionQuery{target: uuid.New(), child: uuid.New(), result: uuid.New(), sha: hex.EncodeToString(sum[:]), body: raw, operation: "semantic.adopt_shown_retouch_region"}
	task := uuid.NewSHA1(q.child, []byte(q.operation+":"+q.target.String()))
	q.version = uuid.NewSHA1(task, []byte("V27.ShownAppliedVersion"))
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	got, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw)
	if err != nil || got != q.version || q.calls != 2 {
		t.Fatalf("version=%v calls=%d error=%v", got, q.calls, err)
	}
	q.operation = "semantic.preview_retouch_region"
	q.calls = 0
	if _, err := MaterializeShownAppliedVersion(context.Background(), q, who, uuid.New(), q.target, raw); !errors.Is(err, ErrInvalid) || q.calls != 1 {
		t.Fatalf("non-copy producer reached mutation: calls=%d error=%v", q.calls, err)
	}
}

func TestShownVersionCancellationAfterMutationDoesNotAcknowledge(t *testing.T) {
 fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
 if err != nil { t.Fatal(err) }
 var images []json.RawMessage
 if json.Unmarshal(fixture, &images) != nil || len(images) == 0 { t.Fatal("fixture") }
 raw := []byte(images[0]); sum := sha256.Sum256(raw)
 ctx, cancel := context.WithCancel(context.Background()); defer cancel()
 q := &shownVersionQuery{target:uuid.New(), child:uuid.New(), result:uuid.New(), sha:hex.EncodeToString(sum[:]), body:raw, cancelAfterMutation:cancel}
 task := uuid.NewSHA1(q.child, []byte("semantic.adopt_shown_exposure:"+q.target.String()))
 q.version = uuid.NewSHA1(task, []byte("V27.ShownAppliedVersion"))
 got, err := MaterializeShownAppliedVersion(ctx,q,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),q.target,raw)
 if !errors.Is(err,context.Canceled) || got != uuid.Nil || q.calls != 2 { t.Fatalf("version=%v calls=%d err=%v",got,q.calls,err) }
}
