package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeInputDetectorRecoveryRow struct {
	config, profile  []byte
	sha, inputDigest string
	task             uuid.UUID
	params           v2input.Ref
}

func (s SQLTransferSourceReader) readNativeInputDetectorRecoveryRow(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (nativeInputDetectorRecoveryRow, error) {
	return s.readNativePreparationRecoveryRow(ctx, who, tenantID, parent, "face.detect")
}

func (s SQLTransferSourceReader) readNativePreparationRecoveryRow(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, operation string) (nativeInputDetectorRecoveryRow, error) {
	var zero, row nativeInputDetectorRecoveryRow
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || parent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	run, _, _, err := nativeInputSavedStageIdentity(parent, operation)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash,task_id,params_artifact_id,params_sha,input_digest,execution_profile FROM photo.semantic_native_input_saved_stage($1,$2,$3,$4,$5,$6,$7)", tenantID, s.ProjectID, who.UserID, who.SessionID, parent, run, operation).Scan(&row.config, &row.sha, &row.task, &row.params.ArtifactID, &row.params.SHA256, &row.inputDigest, &row.profile)
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return row, nil
}

// Initial parsing yields only bounded locators. Full saved-stage decoding must
// follow physical intent loading and exact original request recompilation.
func nativeInputDetectorAnchorLocators(row nativeInputDetectorRecoveryRow) (string, v2input.Ref, v2input.Ref, error) {
	return nativePreparationAnchorLocators(row, "face.detect")
}

func nativePreparationAnchorLocators(row nativeInputDetectorRecoveryRow, operation string) (string, v2input.Ref, v2input.Ref, error) {
	var zero v2input.Ref
	if len(row.config) < 2 || len(row.config) > 4<<20 {
		return "", zero, zero, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(json.RawMessage(row.config))
	if err != nil || sha != row.sha {
		return "", zero, zero, v2input.ErrConflict
	}
	marker := "v27_detector_prepare"
	if operation == "mask.person" {
		marker = "v27_person_prepare"
	} else if operation != "face.detect" {
		return "", zero, zero, v2input.ErrInvalid
	}
	var envelope map[string]json.RawMessage
	var stage struct {
		Intent   v2input.Ref     `json:"intent"`
		Snapshot v2input.Ref     `json:"snapshot"`
		Recovery json.RawMessage `json:"recovery"`
	}
	if json.Unmarshal(row.config, &envelope) != nil || json.Unmarshal(envelope[marker], &stage) != nil {
		return "", zero, zero, v2input.ErrInvalid
	}
	m, err := decodeNativePlanRecoveryMetadata(stage.Recovery, v2input.DraftTask{Operation: operation, ExecutionProfile: row.profile})
	if err != nil {
		return "", zero, zero, err
	}
	if m.RequestKey == "" || stage.Intent.ArtifactID == uuid.Nil || stage.Snapshot.ArtifactID == uuid.Nil {
		return "", zero, zero, v2input.ErrConflict
	}
	for _, digest := range []string{stage.Intent.SHA256, stage.Snapshot.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return "", zero, zero, v2input.ErrInvalid
		}
	}
	return m.RequestKey, stage.Intent, stage.Snapshot, nil
}

func (s SQLTransferSourceReader) nativeInputSavedAnchorObject(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, configSHA, kind string, ref v2input.Ref) (defectEvidenceObject, error) {
	return s.nativePreparationAnchorObject(ctx, who, tenantID, parent, configSHA, kind, ref, "face.detect")
}

func (s SQLTransferSourceReader) nativePreparationAnchorObject(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, configSHA, kind string, ref v2input.Ref, operation string) (defectEvidenceObject, error) {
	var zero, pin defectEvidenceObject
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	limit := int64(1 << 20)
	if kind == "snapshot" {
		limit = 8 << 20
	} else if kind != "intent" {
		return zero, v2input.ErrInvalid
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	if operation != "face.detect" && operation != "mask.person" {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		if operation == "mask.person" {
			return tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_person_saved_anchor_document($1,$2,$3,$4,$5,$6,$7)", tenantID, s.ProjectID, who.UserID, who.SessionID, parent, configSHA, kind).Scan(&pin.ref.ArtifactID, &pin.ref.SHA256, &bucket, &pin.object.Key, &pin.object.VersionID, &pin.object.Bytes, &pin.object.ContentType)
		}
		return tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_native_input_saved_anchor_document($1,$2,$3,$4,$5,$6,$7)", tenantID, s.ProjectID, who.UserID, who.SessionID, parent, configSHA, kind).Scan(&pin.ref.ArtifactID, &pin.ref.SHA256, &bucket, &pin.object.Key, &pin.object.VersionID, &pin.object.Bytes, &pin.object.ContentType)
	})
	if err != nil {
		return zero, err
	}
	if pin.ref != ref || bucket != s.Bucket || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.Bytes < 2 || pin.object.Bytes > limit || pin.object.ContentType != "application/json" {
		return zero, v2input.ErrConflict
	}
	pin.object.SHA256 = ref.SHA256
	return pin, nil
}

// Rebuilds server-owned continuation context without an in-memory request or
// client body. No publication, graph repair or parent admission occurs here.
func (s SQLTransferSourceReader) loadNativePreparationAnchor(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, color TransferColorProfile, budget int64) (nativePreparationAnchor, error) {
	return s.loadNativePreparationAnchorForOperation(ctx, who, tenantID, parent, color, budget, "face.detect")
}

func (s SQLTransferSourceReader) loadPersonPreparationAnchor(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, color TransferColorProfile, budget int64) (nativePreparationAnchor, error) {
	return s.loadNativePreparationAnchorForOperation(ctx, who, tenantID, parent, color, budget, "mask.person")
}

func (s SQLTransferSourceReader) loadNativePreparationAnchorForOperation(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID, color TransferColorProfile, budget int64, operation string) (nativePreparationAnchor, error) {
	var zero nativePreparationAnchor
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if budget <= 0 || budget > 9007199254740991 || s.Objects == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return zero, v2input.ErrInvalid
	}
	row, err := s.readNativePreparationRecoveryRow(ctx, who, tenantID, parent, operation)
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, budget, len(row.config)+len(row.profile)); err != nil {
		return zero, err
	}
	key, intent, snapshot, err := nativePreparationAnchorLocators(row, operation)
	if err != nil {
		return zero, err
	}
	intentObject, err := s.nativePreparationAnchorObject(ctx, who, tenantID, parent, row.sha, "intent", intent, operation)
	if err != nil {
		return zero, err
	}
	if err := chargeMetadata(&used, budget, int(intentObject.object.Bytes)); err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, intentObject.object, intent)
	if err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenantID, who.UserID, key, body)
	if err != nil {
		return zero, err
	}
	if request.RunID != parent {
		return zero, v2input.ErrConflict
	}
	saved, err := decodeNativeInputSavedStage(row.config, row.sha, row.task, row.params, row.inputDigest, row.profile, s.ProjectID, who, request, intent, snapshot, operation)
	if err != nil {
		return zero, err
	}
	var published PublishedIntent
	if operation == "mask.person" {
		// The intent was physically read through the original person checkpoint
		// above. A standalone person start has no detector graph to authorize it.
		inventory, inventoryErr := ReadInventory(ctx, s.DB, who, request)
		if inventoryErr != nil {
			return zero, inventoryErr
		}
		if inventory.Scope.ProjectID != s.ProjectID {
			return zero, v2input.ErrConflict
		}
		published = PublishedIntent{Ref: intent, Inventory: inventory}
	} else {
		published, err = s.readNativeInputSavedPublishedIntent(ctx, who, request, budget-used)
		if err != nil {
			return zero, err
		}
	}
	if published.Ref != intent {
		return zero, v2input.ErrConflict
	}
	if err := chargeMetadata(&used, budget, len(published.Inventory.Targets)*512+int(intentObject.object.Bytes)); err != nil {
		return zero, err
	}
	snapshotObject, err := s.nativePreparationAnchorObject(ctx, who, tenantID, parent, row.sha, "snapshot", snapshot, operation)
	if err != nil {
		return zero, err
	}
	if err := chargeMetadata(&used, budget, int(snapshotObject.object.Bytes)); err != nil {
		return zero, err
	}
	snapshotBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, snapshotObject.object, snapshot)
	if err != nil {
		return zero, err
	}
	targets, err := BindTargets(request, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(targets, published.Inventory.Targets) {
		return zero, v2input.ErrConflict
	}
	// IO may race cancellation, inventory changes or locator replacement.
	for _, item := range []struct {
		kind string
		pin  defectEvidenceObject
	}{{"intent", intentObject}, {"snapshot", snapshotObject}} {
		after, err := s.nativePreparationAnchorObject(ctx, who, tenantID, parent, row.sha, item.kind, item.pin.ref, operation)
		if err != nil {
			return zero, err
		}
		if after != item.pin {
			return zero, v2input.ErrConflict
		}
	}
	after, err := s.readNativeInputSavedStage(ctx, who, request, intent, snapshot, operation)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) {
		return zero, v2input.ErrConflict
	}
	current, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return zero, err
	}
	if !sameTransferInventory(current, published.Inventory) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativePreparationAnchor{Request: request, Key: key, Published: published, Snapshot: snapshot, SnapshotBody: append([]byte(nil), snapshotBody...), Color: color}, nil
}
