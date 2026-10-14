package v2draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type identityPinTx struct {
	pgx.Tx
	pins   map[string]objectstore.Object
	rotate string
	reads  int
}

func (t *identityPinTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "set_config('app.tenant_id'") {
		return pgconn.CommandTag{}, errors.New("unexpected SQL")
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (t *identityPinTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if sql == "SELECT current_user" {
		return identityPinRow{role: true}
	}
	var role string
	switch {
	case strings.Contains(sql, "v23_identity_consent_result_input"):
		role = "consents"
	case strings.Contains(sql, "v23_identity_calibration_result_input"):
		role = "calibration"
	case strings.Contains(sql, "v23_identity_result_input"), strings.Contains(sql, "v23_identity_result_object"):
		role = args[5].(string)
	default:
		return identityPinRow{err: errors.New("unexpected locator")}
	}
	if strings.Contains(sql, "v23_identity_result_object") {
		role = "output_" + role
	}
	object, ok := t.pins[role]
	if !ok {
		return identityPinRow{err: errors.New("unregistered role")}
	}
	if role == t.rotate {
		object.VersionID = "rotated"
	}
	raw, err := canonical.Marshal(map[string]any{"key": object.Key, "version": object.VersionID,
		"sha256": object.SHA256, "bytes": object.Bytes, "media_type": object.ContentType})
	t.reads++
	return identityPinRow{raw: raw, err: err}
}

type identityPinRow struct {
	role bool
	raw  []byte
	err  error
}

func (r identityPinRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.role {
		*dest[0].(*string) = "photo_scheduler"
	} else {
		*dest[0].(*[]byte) = r.raw
	}
	return nil
}

func TestIdentityObjectPinsTransactionRejectsRotatedNPY(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)}
	}
	pin := func(role string, item v2input.Ref) objectstore.Object {
		media := "application/json"
		if strings.HasSuffix(role, "pixels") {
			media = "application/x-npy"
		}
		return objectstore.Object{Key: role, VersionID: "v1", SHA256: item.SHA256,
			Bytes: 128, ContentType: media}
	}
	scope := IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(), Root: ref()}
	e := PreparedIdentityAdoption{Scope: scope, verified: true, PixelObjects: map[string]objectstore.Object{}}
	e.Inputs.ParamsRef = ref()
	e.Documents.BeforeRef, e.Documents.AfterRef = ref(), ref()
	e.Documents.EffectiveRef, e.Documents.ProtectedRef = ref(), ref()
	e.Consents.Ref, e.Calibration.Ref = ref(), ref()
	e.Documents.BeforePixels, e.Documents.AfterPixels = ref(), ref()
	e.Documents.EffectivePixels, e.Documents.ProtectedPixels = ref(), ref()
	e.Result.Report, e.Result.Mask, e.Result.Pixels = scope.Root, ref(), ref()
	pins := map[string]objectstore.Object{}
	set := func(role string, item v2input.Ref, target *objectstore.Object) {
		value := pin(role, item)
		pins[role] = value
		*target = value
	}
	set("params", e.Inputs.ParamsRef, &e.Inputs.Objects[0])
	set("before", e.Documents.BeforeRef, &e.Documents.Objects[0])
	set("after", e.Documents.AfterRef, &e.Documents.Objects[1])
	set("effective", e.Documents.EffectiveRef, &e.Documents.Objects[2])
	set("protected", e.Documents.ProtectedRef, &e.Documents.Objects[3])
	set("consents", e.Consents.Ref, &e.Consents.Object)
	set("calibration", e.Calibration.Ref, &e.Calibration.Object)
	for _, item := range []struct {
		role string
		ref  v2input.Ref
	}{
		{"before_pixels", e.Documents.BeforePixels}, {"after_pixels", e.Documents.AfterPixels},
		{"effective_pixels", e.Documents.EffectivePixels}, {"protected_pixels", e.Documents.ProtectedPixels}} {
		e.PixelObjects[item.role] = pin(item.role, item.ref)
		pins[item.role] = e.PixelObjects[item.role]
	}
	for i, item := range []struct {
		role string
		ref  v2input.Ref
	}{
		{"report", e.Result.Report}, {"mask", e.Result.Mask}, {"pixels", e.Result.Pixels}} {
		value := pin(item.role, item.ref)
		e.Result.Objects[i] = value
		pins["output_"+item.role] = value
	}
	tx := &identityPinTx{pins: pins}
	if err := CheckIdentityObjectPinsInTx(context.Background(), tx, e, "photos"); err != nil {
		t.Fatal(err)
	}
	if tx.reads != 14 {
		t.Fatalf("checked %d pins, want 14", tx.reads)
	}
	tx.rotate = "before_pixels"
	if err := CheckIdentityObjectPinsInTx(context.Background(), tx, e, "photos"); err != v2input.ErrExpired {
		t.Fatalf("rotated physical input accepted: %v", err)
	}
}
