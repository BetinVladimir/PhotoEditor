-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v29_replacement_detector_adoption_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_replacement_detector_adoption_receipt_scope CHECK (
 v29_replacement_detector_adoption_receipt IS NULL OR
 ((config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND jsonb_typeof(v29_replacement_detector_adoption_receipt)='object'
 AND pg_column_size(v29_replacement_detector_adoption_receipt)<=8388608));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_replacement_detector_adoption_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_replacement_detector_adoption_receipt IS NOT NULL THEN
   RAISE EXCEPTION 'Adoption receipt requires verified commit' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_replacement_detector_adoption_receipt IS DISTINCT FROM OLD.v29_replacement_detector_adoption_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_replacement_detector_adoption_receipt IS NOT NULL
   OR NOT (OLD.config_snapshot ? 'v29_perspective_replacement_detection_jcs') OR NOT OLD.graph_sealed OR OLD.cancel_requested THEN
   RAISE EXCEPTION 'Adoption receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_replacement_detector_adoption_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_replacement_detector_adoption_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v29_replacement_detector_adoption_receipt_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_replacement_detector_adoption_receipt();

CREATE FUNCTION photo.adopt_v29_replacement_detector(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_evidence text,p_params text,p_root text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;expected jsonb;actual jsonb;root jsonb;project uuid;
BEGIN
 IF p_evidence IS NULL OR octet_length(p_evidence) NOT BETWEEN 2 AND 262144
 OR p_root IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 expected:=p_evidence::jsonb;root:=p_root::jsonb;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id;
 IF project IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF r.config_snapshot->>'v29_perspective_replacement_detection_jcs' IS DISTINCT FROM p_proof->>'descriptor_jcs' THEN RETURN false;END IF;
 IF r.v29_replacement_detector_adoption_receipt IS NOT NULL THEN
  RETURN NOT r.cancel_requested AND t.state='succeeded' AND NOT t.cancel_requested
   AND t.canonical_result_id=p_result AND t.attempt=p_attempt AND t.fencing_token=p_fence
   AND r.v29_replacement_detector_adoption_receipt=jsonb_build_object('schema_version',1,
   'task_id',p_task,'result_id',p_result,'attempt',p_attempt,'fence',p_fence,'evidence_jcs',p_evidence,'result_jcs',p_root,
   'params_sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),'inventory_digest',p_digest,
   'sql_digest',encode(sha256(convert_to(p_inventory::text,'UTF8')),'hex'),'descriptor_jcs',p_proof->>'descriptor_jcs')
   AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
   AND event_id=photo.v2_adoption_event_id(p_task,p_result));
 END IF;
 actual:=photo.v29_replacement_detector_adoption_evidence(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_proof);
 IF actual IS NULL OR actual IS DISTINCT FROM expected
 OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM actual->'root'->>'sha256'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(root) k) IS DISTINCT FROM ARRAY['base','lines','schema_version','status']::text[]
 OR root->'schema_version' IS DISTINCT FROM '1'::jsonb OR root->'base' IS DISTINCT FROM actual->'params'->'base'
 OR coalesce(root->>'status','') NOT IN('available','unsupported') THEN RETURN false;END IF;
 IF root->>'status'='unsupported' THEN
  IF root->'lines' IS DISTINCT FROM 'null'::jsonb THEN RETURN false;END IF;
 ELSE
  IF jsonb_typeof(root->'lines'->'lines') IS DISTINCT FROM 'array'
   OR jsonb_array_length(root->'lines'->'lines') NOT BETWEEN 4 AND 128 THEN RETURN false;END IF;
 END IF;
 IF photo.adopt_v2_plan_pre_v29_lines(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.runs SET v29_replacement_detector_adoption_receipt=jsonb_build_object('schema_version',1,
 'task_id',p_task,'result_id',p_result,'attempt',p_attempt,'fence',p_fence,'evidence_jcs',p_evidence,'result_jcs',p_root,
 'params_sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),'inventory_digest',p_digest,
 'sql_digest',encode(sha256(convert_to(p_inventory::text,'UTF8')),'hex'),'descriptor_jcs',p_proof->>'descriptor_jcs')
 WHERE tenant_id=p_tenant AND id=r.id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v29_replacement_detector(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v29_replacement_detector(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.adopt_v29_replacement_detector(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
