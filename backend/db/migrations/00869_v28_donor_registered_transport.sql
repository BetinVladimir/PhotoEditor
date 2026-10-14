-- +goose Up
-- Read retained transport only while current donor authority still holds.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_donor_registered_transport(p_tenant uuid,p_run uuid,p_task uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;plan jsonb;entry jsonb;manifest jsonb;chunks jsonb;body text:='[';separator text:='';sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_donor_descriptor_jcs IS NULL OR r.v28_donor_params_intent->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF NOT FOUND OR photo.v28_donor_registered_inventory_ready(p_tenant,p_run,p_task) IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 plan:=r.v28_donor_descriptor_jcs::jsonb;
 FOR entry IN SELECT value FROM jsonb_array_elements(plan->'entries') LOOP
 body:=body||separator||'{"contract":"'||(entry->>'contract')||'","ref":{"artifact_id":"'||(entry->'ref'->>'artifact_id')||'","sha256":"'||(entry->'ref'->>'sha256')||'"}}';separator:=',';
 END LOOP;
 sha:=encode(sha256(convert_to(body||']','UTF8')),'hex');
 IF t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM sha
 OR t.execution_profile->'v2_entry_count' IS DISTINCT FROM to_jsonb(jsonb_array_length(plan->'entries')) THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureManifest')<>1
 OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureChunk')<>1 THEN RETURN NULL;END IF;
 -- Recheck every retained transport publication and original ready version.
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract IN('InputClosureManifest','InputClosureChunk') AND NOT EXISTS(
 SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task AND d.artifact_id=i.artifact_id
 AND d.purpose=CASE i.contract WHEN 'InputClosureManifest' THEN 'v2_input_manifest' ELSE 'v2_input_chunk_0' END
 AND d.state='ready' AND d.sha256=i.sha256 AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bytes BETWEEN 2 AND 1048576 AND a.bucket=d.bucket AND a.media_type='application/json'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||a.id
 AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.output_role=d.purpose AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a)) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO manifest FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureManifest';
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal) INTO chunks FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureChunk';
 IF t.execution_profile->'v2_input_manifest' IS DISTINCT FROM manifest THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('manifest',manifest,'chunks',chunks,'count',jsonb_array_length(plan->'entries'),'sha256',sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v28_donor_registered_transport(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_donor_registered_transport(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_donor_registered_transport(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
