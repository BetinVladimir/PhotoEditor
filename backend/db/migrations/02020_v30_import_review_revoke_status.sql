-- +goose Up
-- Revoke remains possible after the import run stops. The exact target
-- proposal key and editor authority still apply; archived review is irrelevant.
-- +goose StatementBegin
CREATE FUNCTION photo.revoke_v30_import_review(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_request_id uuid,p_request jsonb)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE w photo.v2_workflows%ROWTYPE;old photo.v30_import_review_decisions%ROWTYPE;
 entry_id uuid;target_version uuid;proposal_id uuid;base_id uuid;label_id uuid;
 proposal_sha text;base_sha text;label_sha text;kind text;expected_epoch bigint;next_epoch bigint;request_hash text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_request_id IS NULL
 OR p_request IS NULL OR jsonb_typeof(p_request) IS DISTINCT FROM 'object'
 OR p_request->>'action' IS DISTINCT FROM 'revoke'
 OR p_request ? 'decision' OR p_request ? 'accepted_candidate_ids'
 OR p_request ? 'rejected_candidate_ids'
 OR coalesce(p_request->>'expected_epoch','') !~ '^[1-9][0-9]*$' THEN RETURN NULL;END IF;
 expected_epoch:=(p_request->>'expected_epoch')::bigint;
 IF expected_epoch>=9007199254740991 THEN RETURN NULL;END IF;
 kind:=p_request->>'kind';
 IF kind IS NULL OR kind NOT IN('defects','dust') THEN RETURN NULL;END IF;
 entry_id:=(p_request->>'entry_id')::uuid;
 target_version:=(p_request->>'target_version_id')::uuid;
 proposal_id:=(p_request#>>'{proposal,artifact_id}')::uuid;
 proposal_sha:=p_request#>>'{proposal,sha256}';
 base_id:=(p_request#>>'{base,artifact_id}')::uuid;
 base_sha:=p_request#>>'{base,sha256}';
 IF entry_id IS NULL OR target_version IS NULL OR proposal_id IS NULL OR base_id IS NULL
 OR coalesce(proposal_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(base_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 IF kind='defects' THEN
  label_id:=(p_request#>>'{label_map,artifact_id}')::uuid;
  label_sha:=p_request#>>'{label_map,sha256}';
  IF label_id IS NULL OR coalesce(label_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 ELSIF p_request ? 'label_map' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND workflow_type='interchange' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO old FROM photo.v30_import_review_decisions d WHERE d.tenant_id=p_tenant
  AND d.workflow_id=p_workflow AND d.project_id=w.project_id AND d.entry_id=entry_id
  AND d.target_version_id=target_version AND d.kind=kind AND d.proposal_id=proposal_id FOR UPDATE;
 IF NOT FOUND OR old.proposal_sha<>proposal_sha OR old.base_id<>base_id OR old.base_sha<>base_sha
  OR old.label_map_id IS DISTINCT FROM label_id OR old.label_map_sha IS DISTINCT FROM label_sha THEN RETURN NULL;END IF;
 request_hash:=encode(sha256(convert_to(p_tenant::text||':'||p_workflow::text||':'||p_actor::text||':'||p_request::text,'UTF8')),'hex');
 IF old.status='revoked' AND old.request_id=p_request_id AND old.request_hash=request_hash THEN RETURN old.epoch;END IF;
 IF old.status<>'granted' OR old.epoch<>expected_epoch THEN RETURN NULL;END IF;
 UPDATE photo.v30_import_review_decisions AS d SET status='revoked',epoch=epoch+1,
  actor_id=p_actor,request_id=p_request_id,request_hash=request_hash,updated_at=clock_timestamp()
 WHERE d.tenant_id=p_tenant AND d.workflow_id=p_workflow AND d.entry_id=entry_id
  AND d.target_version_id=target_version AND d.kind=kind AND d.proposal_id=proposal_id
 RETURNING epoch INTO next_epoch;
 RETURN next_epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_api;

CREATE FUNCTION photo.v30_import_review_status(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_target_version uuid,p_kind text,p_proposal uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;d photo.v30_import_review_decisions%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_entry IS NULL OR p_target_version IS NULL
 OR p_kind NOT IN('defects','dust') OR p_proposal IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND workflow_type='interchange' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_import_review_decisions c WHERE c.tenant_id=p_tenant
  AND c.workflow_id=p_workflow AND c.project_id=w.project_id AND c.entry_id=p_entry
  AND c.target_version_id=p_target_version AND c.kind=p_kind AND c.proposal_id=p_proposal;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('entry_id',d.entry_id,'target_version_id',d.target_version_id,
  'kind',d.kind,'proposal',jsonb_build_object('artifact_id',d.proposal_id,'sha256',d.proposal_sha),
  'base',jsonb_build_object('artifact_id',d.base_id,'sha256',d.base_sha),
  'label_map',CASE WHEN d.label_map_id IS NULL THEN NULL ELSE jsonb_build_object('artifact_id',d.label_map_id,'sha256',d.label_map_sha) END,
  'decision',d.decision,'accepted_candidate_ids',d.accepted_ids,'rejected_candidate_ids',d.rejected_ids,
  'status',d.status,'epoch',d.epoch);
END $$;
ALTER FUNCTION photo.v30_import_review_status(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_status(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_status(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
