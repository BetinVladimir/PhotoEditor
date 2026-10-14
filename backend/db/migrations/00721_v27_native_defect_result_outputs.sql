-- +goose Up
-- Native result keeps the original five verified roles. Generic V2 root contract
-- remains unchanged. Registry enablement and queue activation remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_native_result_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;native jsonb;ref jsonb;ordinal integer;seen uuid[]:='{}';artifact uuid;roles text[]:=ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'];media text[]:=ARRAY['application/octet-stream','application/octet-stream','image/png','application/json','application/json'];
BEGIN
 IF jsonb_typeof(p_outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(p_outputs)<>5 OR octet_length(p_outputs::text)>8192 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation<>'defects.propose' OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.state<>'running' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.execution_profile->>'backend'<>'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 native:=r.config_snapshot->'v27_defect_prepare';
 IF native->'schema_version' IS DISTINCT FROM '1'::jsonb OR native->>'task_id' IS DISTINCT FROM t.id::text OR native->>'input_digest' IS DISTINCT FROM t.input_digest
 OR native->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND workflow_type='semantic' AND renderer_version='v2.2' AND project_id::text=r.config_snapshot->>'project_id') THEN RETURN false;END IF;
 FOR ordinal IN 0..4 LOOP
  ref:=p_outputs->ordinal;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}' OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  artifact:=(ref->>'artifact_id')::uuid;
  IF artifact IS NULL OR artifact='00000000-0000-0000-0000-000000000000'::uuid OR artifact=ANY(seen) THEN RETURN false;END IF;
  seen:=array_append(seen,artifact);
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
   WHERE a.tenant_id=p_tenant AND a.id=artifact AND a.sha256=ref->>'sha256' AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
   AND a.output_role=roles[ordinal+1] AND a.media_type=media[ordinal+1] AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.role=a.output_role AND u.state='ready' AND u.completed_at IS NOT NULL
   AND u.sha256=a.sha256 AND u.bytes=a.bytes AND u.media_type=a.media_type AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version) THEN RETURN false;END IF;
 END LOOP;
 RETURN (SELECT count(*) FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=t.id AND attempt=p_attempt AND fence=p_fence AND state='ready')=5;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_native_result_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_native_result_ready(uuid,uuid,integer,bigint,jsonb) FROM PUBLIC;
CREATE OR REPLACE FUNCTION photo.guard_v2_result_root() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; root jsonb;
BEGIN
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id;
 IF run_row.kind<>'v2' THEN RETURN NEW; END IF;
 IF task_row.operation='defects.propose' AND run_row.config_snapshot ? 'v27_defect_prepare' THEN
  IF photo.semantic_defect_native_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_DEFECT_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=run_row.tenant_id
   AND w.run_id=run_row.id AND w.renderer_version='v2.2') THEN
  RAISE EXCEPTION 'V2_RENDERER_MISMATCH' USING ERRCODE='23514';
 END IF;
 IF jsonb_typeof(NEW.outputs)<>'array' OR jsonb_array_length(NEW.outputs)<>1 THEN
  RAISE EXCEPTION 'V2_ROOT_REQUIRED' USING ERRCODE='23514';
 END IF;
 root:=NEW.outputs->0;
 IF jsonb_typeof(root)<>'object' OR (root-'artifact_id'-'sha256')<>'{}'::jsonb
  OR root->>'artifact_id' IS NULL OR root->>'sha256' IS NULL
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
   WHERE a.tenant_id=NEW.tenant_id AND a.id=(root->>'artifact_id')::uuid
   AND a.sha256=root->>'sha256' AND a.output_role='document' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
   AND a.created_by_task_id=task_row.id AND a.produced_attempt=NEW.attempt
   AND a.produced_fence=NEW.fencing_token AND u.state='ready' AND u.role='document'
   AND u.task_id=task_row.id AND u.attempt=NEW.attempt AND u.fence=NEW.fencing_token) THEN
  RAISE EXCEPTION 'V2_ROOT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
