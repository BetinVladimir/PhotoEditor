package run

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Private mode is constructed only after the frozen V2 closure was read and
// compiled by the server. Public Apply JSON cannot choose or populate this mode.
type editorEffectApplyMode struct {
	AuthoredBase                      *authoredBaseApplyBinding
	ProjectID                         uuid.UUID
	Binding                           v2input.SessionVersionBinding
	FrozenRecipe                      v2input.Ref
	ExecutedStyle                     v2input.Ref
	FrozenDocumentID, DraftDocumentID uuid.UUID
	DraftRevision                     int64
	SourceSHA, CompilationSHA         string
	SourceResult                      *v2input.Ref
	Precompose                        bool
	PrecomposeProfile                 json.RawMessage
	Closure                           []v2input.Ref
	Override                          json.RawMessage
	Intent                            EditorEffectRequest
	ItemID                            uuid.UUID
}
type EditorEffectRequest struct {
	Source           v2input.SessionVersionBinding `json:"source"`
	Recipe           v2input.Ref                   `json:"recipe"`
	FrozenDocumentID uuid.UUID                     `json:"frozen_document_id"`
	DraftDocumentID  uuid.UUID                     `json:"draft_document_id"`
	DraftRevision    int64                         `json:"draft_revision"`
}

func editorNullableID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
func (m *editorEffectApplyMode) authorize(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, lock bool) error {
	if m == nil || m.ProjectID == uuid.Nil || m.Binding.AssetVersionID == uuid.Nil || m.ItemID == uuid.Nil || m.DraftRevision < 1 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if m.AuthoredBase != nil {
		if m.Precompose {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		if err := m.authorizeAuthoredBase(ctx, tx, who, tid); err != nil {
			return err
		}
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT photo.editor_effect_origin_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tid, m.ProjectID, m.Binding.AssetVersionID, m.Binding.Source.ArtifactID, m.Binding.Source.SHA256, m.FrozenRecipe.ArtifactID, m.FrozenRecipe.SHA256, m.FrozenDocumentID, m.DraftDocumentID, m.DraftRevision, who.UserID, who.SessionID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return domain.NewError("SOURCE_VERSION_MISMATCH", 409)
	}
	if m.Precompose {
		var current []byte
		if err = tx.QueryRow(ctx, `SELECT photo.editor_precompose_installation_pin($1,$2,$3,$4)`, tid, m.ProjectID, who.UserID, who.SessionID).Scan(&current); err != nil {
			return err
		}
		var actual map[string]json.RawMessage
		var expected map[string]json.RawMessage
		if json.Unmarshal(current, &actual) != nil || json.Unmarshal(m.PrecomposeProfile, &expected) != nil {
			return domain.NewError("RENDER_UNAVAILABLE", 503)
		}
		a, _ := canonical.Bytes(actual["qualification_pin"])
		b, _ := canonical.Bytes(expected["native_qualification_pin"])
		if len(a) == 0 || !bytes.Equal(a, b) || !bytes.Equal(actual["icc_lock_sha"], expected["icc_lock_sha"]) {
			return domain.NewError("RENDER_UNAVAILABLE", 503)
		}
	}
	if lock {
		if _, err = tx.Exec(ctx, `SELECT v.id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id WHERE v.tenant_id=$1 AND v.id=$2 ORDER BY v.id FOR SHARE OF v,a,ar`, tid, m.Binding.AssetVersionID); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT photo.lock_editor_effect_identity($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tid, m.ProjectID, m.Binding.AssetVersionID, m.Binding.Source.ArtifactID, m.Binding.Source.SHA256, m.FrozenRecipe.ArtifactID, m.FrozenRecipe.SHA256, m.FrozenDocumentID, m.DraftDocumentID, m.DraftRevision, who.UserID, who.SessionID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return domain.NewError("SOURCE_VERSION_MISMATCH", 409)
		}
		if _, err = tx.Exec(ctx, `SELECT user_id FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tid, who.UserID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM photo.projects WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tid, m.ProjectID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT user_id FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3 FOR SHARE`, tid, m.ProjectID, who.UserID); err != nil {
			return err
		}
		return m.authorize(ctx, tx, who, tid, false)
	}
	return nil
}
func (m *editorEffectApplyMode) snapshot(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID) (uuid.UUID, string, []applySelection, error) {
	var item applySelection
	item.ItemID = m.ItemID
	item.AssetVersionID = m.Binding.AssetVersionID
	item.ArtifactID = m.Binding.Source.ArtifactID
	item.SHA256 = m.Binding.Source.SHA256
	err := tx.QueryRow(ctx, `SELECT a.id,original.bytes FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id AND original.state='ready' WHERE v.tenant_id=$1 AND v.id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4 AND a.project_id=$5 AND a.deleted_at IS NULL AND a.upload_state='verified'`, tid, item.AssetVersionID, item.ArtifactID, item.SHA256, m.ProjectID).Scan(&item.AssetID, &item.OriginalBytes)
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	items := []applySelection{item}
	digest, err := canonical.Hash(items)
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	id := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,$2,'v2','user',$3,$4,1,clock_timestamp()+interval '1 hour')`, id, tid, who.UserID, digest); err != nil {
		return uuid.Nil, "", nil, err
	}
	payload, err := canonical.Marshal(map[string]any{"position": 0, "asset_id": item.AssetID, "canvas_hash": m.Binding.CanvasHash})
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,$5,$6,$7)`, tid, id, item.ItemID, item.AssetVersionID, item.ArtifactID, item.SHA256, payload)
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	// Freeze only after writing the complete single-source selection.
	_, err = tx.Exec(ctx, `UPDATE photo.snapshots SET expires_at=NULL WHERE tenant_id=$1 AND id=$2`, tid, id)
	return id, digest, items, err
}
func (m *editorEffectApplyMode) commit(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, accepted ApplyAccepted) error {
	if err := m.authorize(ctx, tx, who, tid, true); err != nil {
		return err
	}
	if m.AuthoredBase != nil {
		return m.commitAuthoredBase(ctx, tx, who, tid, accepted)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO photo.editor_effect_builds(tenant_id,run_id,project_id,actor_id,session_id,asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha,executed_style_artifact_id,executed_style_sha) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17 WHERE EXISTS(SELECT FROM photo.artifacts ar WHERE ar.tenant_id=$1 AND ar.id=$16 AND ar.sha256=$17 AND ar.state='ready' AND ar.verified_at IS NOT NULL AND ar.object_version IS NOT NULL)`, tid, accepted.RunID, m.ProjectID, who.UserID, who.SessionID, m.Binding.AssetVersionID, m.Binding.Source.ArtifactID, m.Binding.Source.SHA256, m.Binding.CanvasHash, m.FrozenRecipe.ArtifactID, m.FrozenRecipe.SHA256, m.FrozenDocumentID, m.DraftDocumentID, m.DraftRevision, m.CompilationSHA, m.ExecutedStyle.ArtifactID, m.ExecutedStyle.SHA256)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.NewError("SOURCE_VERSION_MISMATCH", 409)
	}
	return nil
}

func (m *editorEffectApplyMode) config() any {
	return map[string]any{"original_recipe": m.FrozenRecipe, "source": m.Binding, "source_result": m.SourceResult, "inputs": m.Closure, "execution_profile": m.PrecomposeProfile}
}
