package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// ReadDelegatedSignedDependencyReceipt is a scheduler-only SQL receipt read.
// It proves a current adopted row, not physical signed-object validity.
func ReadDelegatedSignedDependencyReceipt(ctx context.Context,
	db ProofTransaction, lease ExpansionLease, slot Slot,
	dependency SigningDependency, bucket string,
) (SignedDependencyReceipt, bool, error) {
	var receipt SignedDependencyReceipt
	if err := ctx.Err(); err != nil { return receipt, false, err }
	expectedKey := uuid.NewSHA1(lease.Run,
		[]byte(fmt.Sprintf("%d:%s", slot.Ordinal, slot.PresetID))).String()
	if db == nil || !lease.valid() || lease.Cursor != lease.SlotCount ||
		slot.State != "ready" || slot.ExportID == nil || slot.Export == nil ||
		slot.Ordinal != dependency.Ordinal ||
		slot.PresetID != dependency.PresetID ||
		*slot.ExportID != dependency.ExportID ||
		*slot.Export != dependency.Export ||
		dependency.IdempotencyKey != expectedKey ||
		dependency.PolicyID == uuid.Nil ||
		len(bucket) < 3 || len(bucket) > 63 {
		return receipt, false, v2input.ErrInvalid
	}
	err := db.QueryRow(ctx,
		`SELECT signed_id,signed_sha,manifest_id,manifest_sha,validation_sha,
		 adopted_task,adopted_result,bucket,signed_key,signed_version,
		 signed_bytes,signed_media,manifest_key,manifest_version,manifest_bytes
		 FROM photo.v31_delivery_delegated_signed_receipt($1,$2,$3,$4)`,
		lease.Expansion, lease.Token, slot.Ordinal, slot.PresetID).Scan(
		&receipt.Signed.ArtifactID, &receipt.Signed.SHA256,
		&receipt.Manifest.ArtifactID, &receipt.Manifest.SHA256,
		&receipt.ValidationSHA, &receipt.TaskID, &receipt.ResultID,
		&receipt.Bucket, &receipt.SignedObject.Key,
		&receipt.SignedObject.VersionID, &receipt.SignedObject.Bytes,
		&receipt.SignedObject.ContentType, &receipt.ManifestObject.Key,
		&receipt.ManifestObject.VersionID, &receipt.ManifestObject.Bytes)
	if errors.Is(err, pgx.ErrNoRows) { return SignedDependencyReceipt{}, false, nil }
	if err != nil { return SignedDependencyReceipt{}, false, err }
	receipt.SignedObject.SHA256 = receipt.Signed.SHA256
	receipt.ManifestObject.SHA256 = receipt.Manifest.SHA256
	if receipt.Bucket != bucket || !validSignedDependencyReceipt(dependency, receipt) {
		return SignedDependencyReceipt{}, false, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil { return SignedDependencyReceipt{}, false, err }
	return receipt, true, nil
}
