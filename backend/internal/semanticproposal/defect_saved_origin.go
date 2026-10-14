package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// A saved selection is only a restart locator. Callers must reconstruct full
// current qualification, content, person protection and locked admission.
func decodeSavedDefectOrigin(body []byte, project uuid.UUID, who auth.Identity, request Request, intent, snapshot v2input.Ref) (defectOriginSelection, error) {
	var zero defectOriginSelection
	if len(body) < 2 || len(body) > 1<<20 || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.ActorID != who.UserID || request.RunID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	// Reject duplicate keys and noncanonical JSON ambiguities before decoding
	// a persisted identity. PostgreSQL JSONB cannot retain duplicate keys, but
	// this decoder is also exercised independently of the database reader.
	body, err := canonical.Bytes(body)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Project uuid.UUID `json:"project_id"`
		Native  struct {
			Schema     int                           `json:"schema_version"`
			Parent     uuid.UUID                     `json:"parent_run_id"`
			RequestSHA string                        `json:"request_sha"`
			Session    uuid.UUID                     `json:"auth_session_id"`
			Source     v2input.SessionVersionBinding `json:"source"`
			Intent     v2input.Ref                   `json:"intent"`
			Snapshot   v2input.Ref                   `json:"snapshot"`
			Selection  json.RawMessage               `json:"original_producer"`
		} `json:"v27_defect_prepare"`
	}
	if json.Unmarshal(body, &config) != nil || config.Project != project || config.Native.Schema != 1 || config.Native.Parent != request.RunID || config.Native.RequestSHA != request.RequestSHA || config.Native.Session != who.SessionID || config.Native.Source != request.Intent.Reference || config.Native.Intent != intent || config.Native.Snapshot != snapshot {
		return zero, v2input.ErrConflict
	}
	// Old snapshots do not contain this field. They cannot supply an automatic
	// restart locator; their original explicit-evidence path remains separate.
	var fields map[string]json.RawMessage
	if json.Unmarshal(config.Native.Selection, &fields) != nil || len(fields) != 2 || fields["producer_task_id"] == nil || fields["params_sha256"] == nil {
		return zero, v2input.ErrInvalid
	}
	var selected defectOriginSelection
	if json.Unmarshal(config.Native.Selection, &selected) != nil || selected.ProducerID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(selected.ParamsSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	return selected, nil
}

func (s SQLTransferSourceReader) readSavedDefectOrigin(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) (DefectOriginPins, error) {
	var zero DefectOriginPins
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || request.ActorID != who.UserID || request.TenantID == uuid.Nil || intent.ArtifactID == uuid.Nil || snapshot.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if request.Intent.Reference.AssetVersionID == uuid.Nil || request.Intent.Reference.Source.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{request.RequestSHA, request.Intent.Reference.Source.SHA256, request.Intent.Reference.CanvasHash, intent.SHA256, snapshot.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	run, _, err := defectPreparationIDs(request.RunID)
	if err != nil {
		return zero, err
	}
	var body []byte
	var configSHA string
	err = tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT config_snapshot,config_hash FROM photo.semantic_defect_saved_config($1,$2,$3,$4,$5)`, request.TenantID, s.ProjectID, who.UserID, who.SessionID, run).Scan(&body, &configSHA)
	})
	if err != nil {
		return zero, err
	}
	actualSHA, err := canonical.Hash(json.RawMessage(body))
	if err != nil || actualSHA != configSHA {
		return zero, v2input.ErrConflict
	}
	selected, err := decodeSavedDefectOrigin(body, s.ProjectID, who, request, intent, snapshot)
	if err != nil {
		return zero, err
	}
	intentSHA, err := canonical.Hash(request.Intent)
	if err != nil || intentSHA != intent.SHA256 {
		return zero, v2input.ErrInvalid
	}
	return s.discoverDefectOriginForProducer(ctx, who, request.TenantID, request.Intent.Reference, selected.ProducerID, selected.ParamsSHA)
}
