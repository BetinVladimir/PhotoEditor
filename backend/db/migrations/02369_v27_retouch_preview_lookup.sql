-- +goose Up
-- Read-only lookup; ambiguity is rejected instead of selecting an arbitrary run.
-- +goose StatementBegin
CREATE FUNCTION photo.lookup_semantic_retouch_preview(p_tenant uuid,p_intent uuid,p_target uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text)
RETURNS TABLE(preview_id uuid,params jsonb,params_sha text,artifact_id uuid,sha256 text,
 bucket text,object_key text,object_version text,bytes bigint,media_type text,adapter_context_id uuid,adapter_context_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;matches integer:=0;e jsonb;a photo.artifacts%ROWTYPE;
BEGIN
 IF photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
  p_revision,p_target_revision,p_plan_sha,p_candidate_sha) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 FOR r IN SELECT candidate.* FROM photo.runs candidate
 WHERE candidate.tenant_id=p_tenant AND candidate.kind='preview' AND NOT candidate.cancel_requested
 AND candidate.status IN('running','completed') AND candidate.graph_sealed AND candidate.created_by=p_actor
 AND candidate.config_snapshot->'semantic_retouch_preview'->>'actor_id'=p_actor::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->>'session_id'=p_session::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'=p_intent::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->>'target_id'=p_target::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->>'workflow_revision'=p_revision::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->>'target_revision'=p_target_revision::text
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->'plan'->>'sha256'=p_plan_sha
 AND candidate.config_snapshot->'semantic_retouch_preview'->'params'->'candidate'->>'sha256'=p_candidate_sha
 LIMIT 2 LOOP
 matches:=matches+1;
 IF matches>1 THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_AMBIGUOUS' USING ERRCODE='40001';END IF;
 e:=r.config_snapshot->'semantic_retouch_preview'->'params';
 adapter_context_id:=(r.config_snapshot->'semantic_retouch_preview'->'adapter_context_ref'->>'artifact_id')::uuid;
 adapter_context_sha:=r.config_snapshot->'semantic_retouch_preview'->'adapter_context_ref'->>'sha256';
 IF adapter_context_id IS NULL OR adapter_context_id='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(adapter_context_sha,'') !~ '^[0-9a-f]{64}$'
 OR photo.semantic_retouch_preview_run_current(p_tenant,r.id) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 preview_id:=r.id;params:=e;params_sha:=r.config_snapshot->'semantic_retouch_preview'->>'params_sha';
 SELECT * INTO a FROM photo.artifacts object WHERE object.tenant_id=p_tenant
 AND object.id=(e->'source'->'source'->>'artifact_id')::uuid
 AND object.sha256=e->'source'->'source'->>'sha256'
 AND object.state='ready' AND object.verified_at IS NOT NULL AND object.media_type='application/json'
 AND object.bytes BETWEEN 2 AND 1048576 AND object.object_version IS NOT NULL
 AND object.object_version NOT IN('','null') AND (object.expires_at IS NULL OR object.expires_at>clock_timestamp());
 IF a.id IS NULL THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_SOURCE_UNAVAILABLE' USING ERRCODE='42501';END IF;
 artifact_id:=a.id;sha256:=a.sha256;bucket:=a.bucket;object_key:=a.object_key;
 object_version:=a.object_version;bytes:=a.bytes;media_type:=a.media_type;
 END LOOP;
 IF matches=1 THEN RETURN NEXT;END IF;
END $$;
ALTER FUNCTION photo.lookup_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lookup_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.lookup_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
