package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type identityPinRow struct{ accepted bool }

func (r identityPinRow) Scan(dest ...any) error { *dest[0].(*bool) = r.accepted; return nil }

type identityPinDB struct {
	calls    int
	query    string
	args     []any
	accepted bool
}

func (d *identityPinDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	d.query = query
	d.args = append([]any(nil), args...)
	return identityPinRow{d.accepted}
}

func TestIdentityCompareConsentPinBindsTaskAndExactDocument(t *testing.T) {
	tenant, workflow := uuid.New(), uuid.New()
	body := []byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`)
	sum := sha256.Sum256(body)
	ref := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	db := &identityPinDB{accepted: true}
	pinner := SQLIdentityCompareConsentPinner{DB: db}
	if err := pinner.Pin(context.Background(), tenant, workflow, 3, ref, body); err != nil {
		t.Fatal(err)
	}
	task, err := TaskID(workflow, "identity.compare", 3)
	if err != nil {
		t.Fatal(err)
	}
	if db.calls != 1 || !strings.Contains(db.query, "photo.pin_v23_identity_compare_consents") ||
		db.args[0] != tenant || db.args[1] != workflow || db.args[2] != task || db.args[3] != string(body) {
		t.Fatal("scope or body changed")
	}
	db.accepted = false
	if err := pinner.Pin(context.Background(), tenant, workflow, 3, ref, body); !errors.Is(err, ErrConflict) {
		t.Fatal("SQL refusal", err)
	}
	for name, raw := range map[string][]byte{"changed": append(append([]byte(nil), body...), ' '),
		"invalid grant": []byte(`{"confirmed_faces":[],"grants":[{}],"schema_version":1}`)} {
		before := db.calls
		if err := pinner.Pin(context.Background(), tenant, workflow, 3, ref, raw); err == nil || db.calls != before {
			t.Fatal(name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pinner.Pin(ctx, tenant, workflow, 3, ref, body); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestIdentityCompareConsentPinAcceptsMixedSnapshot(t *testing.T) {
	tenant, workflow, person := uuid.New(), uuid.New(), uuid.New()
	identity, learning := uuid.New(), uuid.New()
	body := []byte(fmt.Sprintf(`{"confirmed_faces":[],"grants":[{"consent_id":"%s","epoch":"2","person_id":"%s","purpose":"identity_matching"},{"consent_id":"%s","epoch":"4","person_id":null,"purpose":"learning_photographer"}],"schema_version":1}`,
		identity, person, learning))
	sum := sha256.Sum256(body)
	ref := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	db := &identityPinDB{accepted: true}
	if err := (SQLIdentityCompareConsentPinner{DB: db}).Pin(context.Background(), tenant, workflow, 0, ref, body); err != nil {
		t.Fatal(err)
	}
	if db.calls != 1 {
		t.Fatal("expected exact snapshot pin")
	}
}
