-- +goose Up
-- Preserve current shell/queue/metadata/quota semantics for the first derivative
-- of a watermark preset; ordinary admission remains unchanged.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)'::regprocedure);
 needle:='photo.create_v30_delivery_child_draft(';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_DERIVATIVE_DRAFT_NAME_DRIFT';END IF;
 definition:=replace(definition,needle,'photo.create_v30_watermark_derivative_initial_draft(');
 needle:='DECLARE expected jsonb;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_DERIVATIVE_DRAFT_DECLARATION_DRIFT';END IF;
 definition:=replace(definition,needle,'DECLARE stage_scope jsonb;expected jsonb;');
 needle:=' task_id:=photo.v30_uuid5(workflow,operation||'':0'');';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_DERIVATIVE_DRAFT_STAGE_DRIFT';END IF;
 definition:=replace(definition,needle,$patch$
 stage_scope:=photo.v30_watermark_derivative_initial_params_scope(p_expansion,p_token,p_slot,p_params);
 IF stage_scope IS NULL OR operation NOT IN('derivative.resize','derivative.sharpen') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=(stage_scope->>'owner')::uuid
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=(p_task->>'params_artifact_id')::uuid
 AND d.sha256=stage_scope->>'sha' AND d.bytes=(stage_scope->>'bytes')::bigint
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 task_id:=photo.v30_uuid5(workflow,operation||':0');
$patch$);
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
