package v2resource

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Load returns the exact V2 Resource envelope. Table names are a fixed
// allowlist; the caller must already hold an authorized tenant transaction.
func Load(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID,
	kind string) (json.RawMessage, error) {
	var query string
	switch kind {
	case "shoot_sessions":
		query = `SELECT jsonb_build_object('id',t.id,'revision',t.revision::text,
		 'document',NULL,'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','shoot_sessions','revision',t.revision::text,
		 'consent_epoch',t.consent_epoch::text))
		 FROM photo.shoot_sessions t WHERE t.tenant_id=$1 AND t.id=$2`
	case "session_analyses":
		query = `SELECT jsonb_build_object('id',t.id,'revision','1',
		 'document',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),
		 'data',(to_jsonb(t)-'plan_body_digest') || jsonb_build_object('resource_type','session_analyses'))
		 FROM photo.session_analyses t JOIN photo.artifacts a
		 ON a.tenant_id=t.tenant_id AND a.id=t.plan_artifact_id
		 WHERE t.tenant_id=$1 AND t.id=$2 AND a.state='ready'
			 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL`
	case "v2_document_streams":
		query = `SELECT jsonb_build_object('id',t.id,'revision',t.revision::text,
		 'document',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),
		 'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','v2_document_streams','revision',t.revision::text))
		 FROM photo.v2_document_streams t JOIN photo.v2_documents d
		 ON d.tenant_id=t.tenant_id AND d.project_id=t.project_id
		 AND d.stream_id=t.id AND d.id=t.current_document_id
		 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id
		 AND a.id=d.artifact_id AND a.sha256=d.sha256
		 WHERE t.tenant_id=$1 AND t.id=$2 AND t.stream_type='draft'
		 AND d.document_type='EditingRecipeV3' AND a.state='ready'
		 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
		 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`
	case "v2_documents":
		query = `SELECT jsonb_build_object('id',t.id,'revision','1',
		 'document',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),
		 'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','v2_documents','version_no',t.version_no::text))
		 FROM photo.v2_documents t JOIN photo.artifacts a
		 ON a.tenant_id=t.tenant_id AND a.id=t.artifact_id
		 AND a.sha256=t.sha256
		 WHERE t.tenant_id=$1 AND t.id=$2 AND a.state='ready'
		 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
		 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`
	case "project_persons":
		query = `SELECT jsonb_build_object('id',t.id,'revision',t.revision::text,
		 'document',NULL,'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','project_persons','revision',t.revision::text,
		 'consent_epoch',t.consent_epoch::text))
		 FROM photo.project_persons t WHERE t.tenant_id=$1 AND t.id=$2`
	case "consent_grants":
		query = `SELECT jsonb_build_object('id',t.id,'revision',t.revision::text,
		 'document',NULL,'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','consent_grants','revision',t.revision::text,
		 'epoch',t.epoch::text))
		 FROM photo.consent_grants t WHERE t.tenant_id=$1 AND t.id=$2`
	case "person_observations":
		query = `SELECT jsonb_build_object('id',t.id,'revision',t.revision::text,
		 'document',NULL,'data',to_jsonb(t) || jsonb_build_object(
		 'resource_type','person_observations','revision',t.revision::text))
		 FROM photo.person_observations t WHERE t.tenant_id=$1 AND t.id=$2`
	default:
		return nil, errors.New("unknown V2 resource kind")
	}
	var body json.RawMessage
	err := tx.QueryRow(ctx, query, tenantID, id).Scan(&body)
	return body, err
}

func ID(raw json.RawMessage) (uuid.UUID, error) {
	var value struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.ID == uuid.Nil {
		return uuid.Nil, errors.New("mutation returned no resource ID")
	}
	return value.ID, nil
}
