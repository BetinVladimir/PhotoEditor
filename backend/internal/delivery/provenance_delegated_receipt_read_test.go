package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type delegatedReceiptDB struct {
	receipt SignedDependencyReceipt
	err error
	calls int
}
func (d *delegatedReceiptDB) QueryRow(context.Context, string, ...any) pgx.Row {
	d.calls++
	return delegatedReceiptRow{d.receipt, d.err}
}
type delegatedReceiptRow struct {
	receipt SignedDependencyReceipt
	err error
}
func (r delegatedReceiptRow) Scan(dest ...any) error {
	if r.err != nil { return r.err }
	*dest[0].(*uuid.UUID) = r.receipt.Signed.ArtifactID
	*dest[1].(*string) = r.receipt.Signed.SHA256
	*dest[2].(*uuid.UUID) = r.receipt.Manifest.ArtifactID
	*dest[3].(*string) = r.receipt.Manifest.SHA256
	*dest[4].(*string) = r.receipt.ValidationSHA
	*dest[5].(*uuid.UUID) = r.receipt.TaskID
	*dest[6].(*uuid.UUID) = r.receipt.ResultID
	*dest[7].(*string) = r.receipt.Bucket
	*dest[8].(*string) = r.receipt.SignedObject.Key
	*dest[9].(*string) = r.receipt.SignedObject.VersionID
	*dest[10].(*int64) = r.receipt.SignedObject.Bytes
	*dest[11].(*string) = r.receipt.SignedObject.ContentType
	*dest[12].(*string) = r.receipt.ManifestObject.Key
	*dest[13].(*string) = r.receipt.ManifestObject.VersionID
	*dest[14].(*int64) = r.receipt.ManifestObject.Bytes
	return nil
}

func TestDelegatedReceiptRequiresExactSlotAndCurrentRead(t *testing.T) {
	sha := digest([]byte("original"))
	exportID := uuid.New()
	preset := uuid.New()
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	lease := ExpansionLease{Expansion: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: uuid.New(),
		Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(),
		Cursor: 1, SlotCount: 1}
	slot := Slot{Ordinal: 0, PresetID: preset, State: "ready",
		ExportID: &exportID, Export: &ref}
	dep := SigningDependency{Ordinal: 0, PresetID: preset, ExportID: exportID,
		Export: ref, PolicyID: uuid.New(), IdempotencyKey: uuid.NewSHA1(
			lease.Run, []byte(fmt.Sprintf("%d:%s", 0, preset))).String()}
	receipt := SignedDependencyReceipt{Signed: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("signed"))},
		Manifest: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("manifest"))},
		ValidationSHA: digest([]byte("validation")), TaskID: uuid.New(),
		ResultID: uuid.New(), Bucket: "photos",
		SignedObject: objectstore.Object{Key: "tenants/x/signed", VersionID: "v1",
			Bytes: 10, ContentType: "image/jpeg"},
		ManifestObject: objectstore.Object{Key: "tenants/x/manifest", VersionID: "v1",
			Bytes: 10, ContentType: "application/c2pa"}}
	db := &delegatedReceiptDB{receipt: receipt}
	got, found, err := ReadDelegatedSignedDependencyReceipt(context.Background(),
		db, lease, slot, dep, "photos")
	if err != nil || !found || got.Signed != receipt.Signed || db.calls != 1 {
		t.Fatal(got, found, err)
	}
	foreign := dep
	foreign.ExportID = uuid.New()
	if _, _, err := ReadDelegatedSignedDependencyReceipt(context.Background(),
		db, lease, slot, foreign, "photos"); err == nil || db.calls != 1 {
		t.Fatal("foreign slot reached SQL", err)
	}
	db.err = pgx.ErrNoRows
	if _, found, err := ReadDelegatedSignedDependencyReceipt(context.Background(),
		db, lease, slot, dep, "photos"); err != nil || found {
		t.Fatal("missing receipt accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadDelegatedSignedDependencyReceipt(ctx, db, lease,
		slot, dep, "photos"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
