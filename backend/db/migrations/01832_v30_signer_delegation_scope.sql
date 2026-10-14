-- +goose Up
-- Narrow scheduler delegation from one frozen Delivery slot to one V31 child.
-- This is a scope reader, not V31 admission or an editor-session surrogate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signer_delegation_scope(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;
 original photo.artifacts%ROWTYPE;frozen jsonb;status text;observed_export uuid;
 observed_task uuid;policy uuid;policy_sha text;child_key text;child_workflow uuid;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_preset IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion
  AND kind='v2.delivery/1' AND state='open' AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id
  AND kind='v2' AND status='running' AND NOT cancel_requested
  AND created_by IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=e.tenant_id
  AND workflow_id=photo.v30_uuid5(e.run_id,'workflow') AND state='building' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=e.tenant_id
  AND package_id=d.id AND project_id=d.project_id AND ordinal=p_ordinal
  AND preset_id=p_preset AND state='ready' AND export_id IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT a.* INTO original FROM photo.exports x JOIN photo.artifacts a
  ON a.tenant_id=x.tenant_id AND a.id=x.artifact_id
  WHERE x.tenant_id=e.tenant_id AND x.id=i.export_id AND x.project_id=d.project_id
   AND x.status='ready' AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.media_type IN('image/jpeg','image/png')
   AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF x,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
  FROM photo.backend_documents b WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id
  AND b.owner_id=e.parent_task_id AND b.purpose='task_params'
  AND b.state='ready' AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_sha' !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 policy:=(frozen->'policy'->>'signing_policy_id')::uuid;
 policy_sha:=frozen->'policy'->>'signing_policy_sha';
 IF policy IS NULL OR photo.v31_signing_policy_ready(e.tenant_id,policy,
  photo.v30_uuid5(e.tenant_id,'signing:policy:'||policy::text||':'||policy_sha),
  policy_sha) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=e.tenant_id
   AND p.id=d.project_id AND p.archived_at IS NULL AND p.download_allowed) THEN RETURN NULL;END IF;
 SELECT signing_status,export_id,task_id INTO status,observed_export,observed_task
  FROM photo.read_v30_delivery_signer_status(p_expansion,p_token,p_ordinal,p_preset);
 IF status NOT IN('not_started','pending') OR observed_export IS DISTINCT FROM i.export_id
  THEN RETURN NULL;END IF;
 child_key:=photo.v30_uuid5(e.run_id,p_ordinal::text||':'||p_preset::text)::text;
 child_workflow:=photo.v30_uuid5(e.tenant_id,'provenance:'||r.created_by::text||':'||child_key);
 IF observed_task IS DISTINCT FROM photo.v30_uuid5(child_workflow,'provenance.sign:0')
  THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant_id',e.tenant_id,'actor_id',r.created_by,
  'project_id',d.project_id,'parent_run_id',e.run_id,'package_id',d.id,
  'ordinal',p_ordinal,'preset_id',p_preset,'export_id',i.export_id,
  'export_artifact_id',original.id,'export_sha',original.sha256,
  'policy_id',policy,'policy_sha',policy_sha,'idempotency_key',child_key,
  'workflow_id',child_workflow,'task_id',observed_task);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signer_delegation_scope(uuid,uuid,integer,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
