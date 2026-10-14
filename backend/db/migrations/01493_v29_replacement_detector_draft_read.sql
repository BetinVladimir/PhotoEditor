-- +goose Up
-- Read-only exact draft recovery. No missing-state repair or admission.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_replacement_detector_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text,p_snapshot uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;config jsonb;detector_run uuid;old photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_replacement_detector_preparation(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original,p_snapshot) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 d:=p_descriptor::jsonb;detector_run:=(d->>'run_id')::uuid;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(d->'base'->'source'->>'artifact_id')::uuid
 AND a.sha256=d->'base'->'source'->>'sha256' AND a.bytes=octet_length(d->>'source_jcs')
 AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',
 jsonb_build_object('version','v29_perspective_replacement_detection_v1','project_id',p_project,'review_id',p_review,
 'decision_hash',p_hash,'original_task_id',d->'replacement'->>'original_task_id','original_run_id',d->'replacement'->>'original_run_id'),
 'v29_perspective_replacement_detection_jcs',p_descriptor);
 SELECT * INTO old FROM photo.runs WHERE id=detector_run FOR UPDATE;
 IF NOT FOUND OR old.tenant_id IS DISTINCT FROM p_tenant OR old.kind IS DISTINCT FROM 'v2'
 OR old.status IS DISTINCT FROM 'draft' OR old.created_by IS DISTINCT FROM p_actor
 OR old.input_snapshot_id IS DISTINCT FROM p_snapshot OR old.failure_policy IS DISTINCT FROM 'fail_fast'
 OR old.graph_sealed OR old.cancel_requested OR old.config_snapshot IS DISTINCT FROM config
 OR old.config_hash IS DISTINCT FROM encode(sha256(convert_to(config::text,'UTF8')),'hex')
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=detector_run) THEN RETURN NULL;END IF;
 RETURN old.config_snapshot->>'v29_perspective_replacement_detection_jcs';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
