-- +goose Up
-- Read committed registered transport only; never publish or repair partial rows.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_relight_registered_transport(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;descriptor text;transport jsonb;manifest jsonb;chunks jsonb;chunk_text text;manifest_body text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT v28_relight_descriptor_jcs INTO descriptor FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF descriptor IS NULL OR descriptor IS DISTINCT FROM p_descriptor
 OR descriptor::jsonb->'task'->>'id' IS DISTINCT FROM p_task::text THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF t.id IS NULL OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 -- Precondition prevents the replay validator from creating missing run quota.
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO manifest
 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureManifest';
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal),
 '['||string_agg('{"artifact_id":"'||artifact_id::text||'","sha256":"'||sha256||'"}',',' ORDER BY ordinal)||']'
 INTO chunks,chunk_text FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureChunk';
 IF manifest IS NULL OR chunks IS NULL THEN RETURN NULL;END IF;
 transport:=jsonb_build_object('manifest',manifest,'chunks',chunks,'count',t.execution_profile->'v2_entry_count','sha256',t.execution_profile->>'v2_entries_sha');
 manifest_body:='{"chunks":'||chunk_text||',"entries_sha":"'||(t.execution_profile->>'v2_entries_sha')||'","entry_count":'||(t.execution_profile->>'v2_entry_count')||',"schema_version":1,"task_id":"'||p_task::text||'"}';
 -- Closure-ready precondition makes01006 a pure exact replay check here. It
 -- repeats current source/qualification/producers/artifact versions/quota and
 -- verifies all stored input and transport rows against the original compiler.
 IF photo.register_v28_relight_transport(p_tenant,p_run,descriptor,p_proof,transport,manifest_body) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN transport;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v28_relight_registered_transport(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_registered_transport(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_registered_transport(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
