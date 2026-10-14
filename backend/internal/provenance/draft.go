package provenance

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

func signingDraftDocuments(who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) ([]byte, []byte, error) {
	if _, err := compileSigningPending(who, ready, prepared, bucket); err != nil {
		return nil, nil, err
	}
	i := prepared.intent
	task := prepared.compiled.Task()
	ref, _ := prepared.compiled.ParamsDocument()
	inputs, err := canonical.Marshal(prepared.compiled.Inputs())
	if err != nil {
		return nil, nil, err
	}
	configValue := map[string]any{"schema_version": 1, "project_id": i.project, "workflow_type": "provenance", "export_id": i.export, "request_sha": i.requestSHA, "snapshot_id": uuid.NewSHA1(i.workflow, []byte("provenance:artifact-snapshot:1")), "snapshot_digest": task.InputDigest, "params": ref}
 if !i.legacyQueueOmitted { configValue["queue_class"] = "batch" }
 config, err := canonical.Marshal(configValue)
	if err != nil {
		return nil, nil, err
	}
	return config, inputs, nil
}

// PrepareSigningDraft pins preparation and creates empty containers in one
// caller-owned API transaction. No object I/O, task or runnable ticket is emitted.
// The caller must discard results if commit fails. Later stages must not use
// this empty-container replay check after materialization/sealing.
func PrepareSigningDraft(ctx context.Context, tx SigningAdmissionTransaction, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return ErrInvalid
	}
	config, inputs, err := signingDraftDocuments(who, ready, prepared, bucket)
	if err != nil {
		return err
	}
	if _, err := PinSigningPending(ctx, tx, who, ready, prepared, bucket); err != nil {
		return err
	}
	i := prepared.intent
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.begin_v31_signing_draft($1,$2,$3,$4,$5,$6,$7)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, string(config), string(inputs)).Scan(&accepted); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}
