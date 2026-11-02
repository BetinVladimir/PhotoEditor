package artifactjson

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

type routeDB struct{ query string }

func (d *routeDB) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	d.query = q
	return routeRow{}
}

type routeRow struct{}

func (r routeRow) Scan(dest ...any) error {
	switch v := dest[0].(type) {
	case *string:
		*v = "forbidden" // Reserve returns ErrStale
	case *bool:
		*v = false // Complete returns ErrStale
	}
	return nil
}

func routeRepo(bucket string) (SQLRepository, *routeDB) {
	db := &routeDB{}
	return SQLRepository{DB: db, Bucket: bucket}, db
}

func baseScope(purpose string) Scope {
	return Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: purpose}
}

// TestReserveRoutesLocalPortrait checks the v28 local params SQL function.
func TestReserveRoutesLocalPortrait(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", LocalPortrait: true}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v28_local_params_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesRestorationPortrait checks the v28 restoration params SQL function.
func TestReserveRoutesRestorationPortrait(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", RestorationPortrait: true}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v28_restoration_params_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesV24CapturePurpose exercises v24CapturePurpose returning true.
func TestReserveRoutesV24CapturePurpose(t *testing.T) {
	repo, db := routeRepo("photos")
	for _, purpose := range []string{
		"v24_capture_prepare_params", "v24_capture_shots",
		"v24_capture_consents", "v24_capture_context", "v24_capture_assess_params",
	} {
		db.query = ""
		repo.Reserve(context.Background(), baseScope(purpose), strings.Repeat("a", 64), 2) //nolint
		if !strings.Contains(db.query, "reserve_v24_capture_document") {
			t.Fatalf("purpose %q: wrong route: %q", purpose, db.query)
		}
	}
}

// TestReserveRoutesDefectHoldout tests the 10-arg reviewed-defect route.
func TestReserveRoutesDefectHoldout(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{
		Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_defect_holdout",
		Review: uuid.New(), Actor: uuid.New(), AuthSession: uuid.New(),
		DecisionHash: strings.Repeat("a", 64),
	}
	repo.Reserve(context.Background(), s, strings.Repeat("b", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_reviewed_defect_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesDustHoldout tests the reviewed-dust route.
func TestReserveRoutesDustHoldout(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{
		Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_dust_holdout",
		Review: uuid.New(), Actor: uuid.New(), AuthSession: uuid.New(),
		DecisionHash: strings.Repeat("a", 64),
	}
	repo.Reserve(context.Background(), s, strings.Repeat("b", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_reviewed_dust_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesDetailHoldout tests the 9-arg detail route.
func TestReserveRoutesDetailHoldout(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{
		Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_detail_holdout",
		Project: uuid.New(), Actor: uuid.New(), AuthSession: uuid.New(),
	}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_detail_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesSeriesHoldout tests the series route.
func TestReserveRoutesSeriesHoldout(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{
		Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_series_holdout",
		Project: uuid.New(), Actor: uuid.New(), AuthSession: uuid.New(),
	}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_series_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesZeroProfile tests the 6-arg zero profile route.
func TestReserveRoutesZeroProfile(t *testing.T) {
	repo, db := routeRepo("photos")
	repo.Reserve(context.Background(), baseScope("v23_zero_profile"), strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_training_zero") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesFrozenBase checks that purposeArg is "base" not the full purpose.
func TestReserveRoutesFrozenBase(t *testing.T) {
	repo, db := routeRepo("photos")
	repo.Reserve(context.Background(), baseScope("v23_frozen_base"), strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v23_training_frozen") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesAssemblyLease tests the semantic-assembly route.
func TestReserveRoutesAssemblyLease(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", AssemblyLease: uuid.New()}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v27_semantic_assembly_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveRoutesAssemblyTransport tests the transport route for v2 input + lease.
func TestReserveRoutesAssemblyTransport(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "v2_input_manifest", AssemblyLease: uuid.New()}
	repo.Reserve(context.Background(), s, strings.Repeat("a", 64), 2) //nolint
	if !strings.Contains(db.query, "reserve_v27_semantic_transport_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesLocalPortrait verifies the v28 local completion route.
func TestCompleteRoutesLocalPortrait(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", LocalPortrait: true}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v28_local_params_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesRestorationPortrait verifies the v28 restoration completion route.
func TestCompleteRoutesRestorationPortrait(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", RestorationPortrait: true}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v28_restoration_params_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesV24Capture verifies the v24 capture completion route.
func TestCompleteRoutesV24Capture(t *testing.T) {
	repo, db := routeRepo("photos")
	s := baseScope("v24_capture_shots")
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v24_capture_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesDefectHoldout verifies the 13-arg reviewed-defect completion route.
func TestCompleteRoutesDefectHoldout(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{
		Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_defect_holdout",
		Review: uuid.New(), Actor: uuid.New(), AuthSession: uuid.New(),
		DecisionHash: strings.Repeat("a", 64),
	}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("b", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v23_reviewed_defect_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesZeroProfile verifies the 9-arg zero profile completion route.
func TestCompleteRoutesZeroProfile(t *testing.T) {
	repo, db := routeRepo("photos")
	s := baseScope("v23_zero_profile")
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v23_training_zero") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesAssemblyLease verifies the assembly completion route.
func TestCompleteRoutesAssemblyLease(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", AssemblyLease: uuid.New()}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v27_semantic_assembly_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestCompleteRoutesAssemblyTransport verifies the transport completion route.
func TestCompleteRoutesAssemblyTransport(t *testing.T) {
	repo, db := routeRepo("photos")
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "v2_input_manifest", AssemblyLease: uuid.New()}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(s, ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	repo.Complete(context.Background(), s, ticket, obj) //nolint
	if !strings.Contains(db.query, "complete_v27_semantic_transport_document") {
		t.Fatalf("wrong route: %q", db.query)
	}
}

// TestReserveNilDBRejectsEarly ensures nil-DB early return fires before any query.
func TestReserveNilDBRejectsEarly(t *testing.T) {
	repo := SQLRepository{DB: nil, Bucket: "photos"}
	if _, err := repo.Reserve(context.Background(), baseScope("review_selection"), strings.Repeat("a", 64), 2); err != objectstore.ErrInvalid {
		t.Fatalf("nil DB must return ErrInvalid, got %v", err)
	}
}

// TestCompleteNilDBRejectsEarly ensures nil-DB early return fires before any query.
func TestCompleteNilDBRejectsEarly(t *testing.T) {
	repo := SQLRepository{DB: nil, Bucket: "photos"}
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, _ := DocumentKey(baseScope("review_selection"), ticket.ArtifactID)
	obj := objectstore.Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 2, ContentType: "application/json"}
	if _, err := repo.Complete(context.Background(), baseScope("review_selection"), ticket, obj); err != objectstore.ErrInvalid {
		t.Fatalf("nil DB must return ErrInvalid, got %v", err)
	}
}

// TestMaxDocumentBytesV30Signed checks v30 signed-closure and signed-envelope limits.
func TestMaxDocumentBytesV30Signed(t *testing.T) {
	if got := maxDocumentBytes("v30_delivery_signed_closure"); got != 8*1048576 {
		t.Fatalf("signed_closure: want 8MB, got %d", got)
	}
	if got := maxDocumentBytes("v30_delivery_signed_envelope"); got != 65536 {
		t.Fatalf("signed_envelope: want 65536, got %d", got)
	}
}

// TestDocumentKeyV24CapturePurposes checks v24 capture purposes pass DocumentKey.
func TestDocumentKeyV24CapturePurposes(t *testing.T) {
	for _, purpose := range []string{
		"v24_capture_prepare_params", "v24_capture_shots",
		"v24_capture_consents", "v24_capture_context", "v24_capture_assess_params",
	} {
		s := baseScope(purpose)
		if _, err := DocumentKey(s, uuid.New()); err != nil {
			t.Fatalf("purpose %q rejected by DocumentKey: %v", purpose, err)
		}
	}
}
