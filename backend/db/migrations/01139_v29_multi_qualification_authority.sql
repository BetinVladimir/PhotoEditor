-- +goose Up
-- Canonical private multi CPU policy helper; does not admit any task.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb; calibrations jsonb; evidence jsonb; encoded text; values_array text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation NOT IN ('optical.panorama','optical.focus_stack') OR p.operation_version<>'1' OR p.backend<>'cpu' OR NOT p.license_cleared
 OR p.required_checks IS NULL OR cardinality(p.required_checks) NOT BETWEEN 1 AND 1000 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p.required_checks) x WHERE x IS NULL OR octet_length(x) NOT BETWEEN 1 AND 100)
 OR cardinality(p.required_checks)<>(SELECT count(DISTINCT x) FROM unnest(p.required_checks) x) THEN RETURN NULL;END IF;
 IF p.test_fixture_shas IS NULL OR p.evidence_shas IS NULL OR p.calibration_shas IS NULL OR cardinality(p.calibration_shas)<1 OR cardinality(p.test_fixture_shas)<1 OR cardinality(p.evidence_shas)<1 THEN RETURN NULL;END IF;
 FOR values_array IN SELECT p.test_fixture_shas UNION ALL SELECT p.calibration_shas UNION ALL SELECT p.evidence_shas LOOP
 IF values_array IS NULL OR cardinality(values_array) NOT BETWEEN 0 AND 10000 OR EXISTS(SELECT 1 FROM unnest(values_array) x WHERE x IS NULL OR x !~ '^[0-9a-f]{64}$')
 OR cardinality(values_array)<>(SELECT count(DISTINCT x) FROM unnest(values_array) x) THEN RETURN NULL;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM unnest(p.test_fixture_shas) x WHERE x=ANY(p.calibration_shas)) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO checks FROM unnest(p.required_checks) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO fixtures FROM unnest(p.test_fixture_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO calibrations FROM unnest(p.calibration_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO evidence FROM unnest(p.evidence_shas) x;
 encoded:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,
 'backend',p.backend,'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,'dataset_sha',p.dataset_sha,
 'issuer_id',p.issuer_id,'required_checks',checks,'test_fixture_shas',fixtures,'calibration_shas',coalesce(calibrations,'[]'::jsonb),'evidence_shas',evidence,'license_cleared',p.license_cleared));
 RETURN encode(sha256(convert_to(encoded,'UTF8')),'hex');
END $$;
ALTER FUNCTION photo.v29_multi_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_policy_sha(uuid) FROM PUBLIC;
-- +goose StatementEnd


-- Exact current multi policy/report/calibration lock; original bytes verified separately.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_multi_qualification(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb,p_operation text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb:=p_pin; field text; verified uuid;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN ('optical.panorama','optical.focus_stack') THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(pin) IS DISTINCT FROM 'object' OR pg_column_size(pin)>16384
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT(pin ?& ARRAY['policy_id','qualification_id','policy_sha','report','calibration','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend'])
 OR (pin-'policy_id'-'qualification_id'-'policy_sha'-'report'-'calibration'-'model_lock_sha'-'runtime_lock_sha'-'device_profile_sha'-'dataset_sha'-'issuer_id'-'backend')<>'{}'::jsonb
 OR jsonb_typeof(pin->'report') IS DISTINCT FROM 'object' OR jsonb_typeof(pin->'calibration') IS DISTINCT FROM 'object'
 OR NOT(pin->'report' ?& ARRAY['artifact_id','sha256']) OR NOT(pin->'calibration' ?& ARRAY['artifact_id','sha256'])
 OR ((pin->'report')-'artifact_id'-'sha256')<>'{}'::jsonb OR ((pin->'calibration')-'artifact_id'-'sha256')<>'{}'::jsonb
 OR pin->>'backend' IS DISTINCT FROM 'cpu' OR coalesce(pin->>'policy_sha','') !~ '^[0-9a-f]{64}$'
 OR pin->'report'->>'artifact_id'=pin->'calibration'->>'artifact_id' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['policy_id','qualification_id','policy_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend']) LOOP
 IF jsonb_typeof(pin->field) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT unnest(ARRAY['report','calibration']) LOOP
 IF jsonb_typeof(pin->field->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(pin->field->'sha256') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT q.id INTO verified FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND calibration.id=(pin->'calibration'->>'artifact_id')::uuid
 WHERE p.id=(pin->>'policy_id')::uuid AND p.operation=p_operation AND p.operation_version='1'
 AND p.backend='cpu' AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.model_lock_sha=pin->>'model_lock_sha' AND p.runtime_lock_sha=pin->>'runtime_lock_sha'
 AND p.device_profile_sha=pin->>'device_profile_sha' AND p.dataset_sha=pin->>'dataset_sha' AND p.issuer_id=(pin->>'issuer_id')::uuid
 AND photo.v29_multi_policy_sha(p.id)=pin->>'policy_sha'
 AND q.operation=p.operation AND q.operation_version=p.operation_version AND q.backend=p.backend
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 AND q.trusted_import AND q.status='passed' AND report.id=(pin->'report'->>'artifact_id')::uuid AND report.sha256=pin->'report'->>'sha256'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=p.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 AND calibration.sha256=pin->'calibration'->>'sha256' AND calibration.sha256=ANY(p.calibration_shas)
 AND calibration.state='ready' AND calibration.verified_at IS NOT NULL AND calibration.media_type='application/json' AND calibration.bytes BETWEEN 2 AND 1048576
 AND calibration.object_version IS NOT NULL AND calibration.object_version NOT IN('','null') AND (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp()) FOR SHARE OF p,q,report,calibration;
 RETURN verified IS NOT NULL AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);

EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;

ALTER FUNCTION photo.lock_v29_multi_qualification(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_multi_qualification(uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_multi_qualification(uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd


-- Live attempt gate only; source producer authority remains mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_lease_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;slot photo.resource_slots%ROWTYPE;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
  OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1
  OR p_sha IS NULL OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$'
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.worker_registrations WHERE tenant_id=p_tenant
  AND worker_id=p_worker AND db_principal=session_user AND enabled) THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true
  OR r.cancel_requested IS DISTINCT FROM false OR r.status NOT IN('queued','running')
  OR t.id IS NULL OR t.operation NOT IN ('optical.panorama','optical.focus_stack')
  OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
  OR t.state IS DISTINCT FROM 'running' OR t.cancel_requested IS DISTINCT FROM false
  OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
  OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
  OR t.input_digest IS DISTINCT FROM p_digest
  OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence
  OR slot.resource_class IS DISTINCT FROM 'cpu'
  OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
  OR photo.worker_authorized(p_tenant,p_worker,'cpu',slot.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') IS DISTINCT FROM true
  OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
  AND w.renderer_version='v2.2') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='Params'
   AND i.artifact_id=p_params AND i.sha256=p_sha AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type='application/json' AND a.bytes=octet_length(p_json)
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 -- Qualification content/current policy and source scope remain separate gates.
 IF jsonb_typeof(t.execution_profile->'multi_qualification') IS DISTINCT FROM 'object'
 OR t.execution_profile->'multi_qualification'->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->'multi_qualification'->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'multi_runtime_sha'
 OR coalesce(t.execution_profile->>'multi_runtime_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM (VALUES ('Report'::text,t.execution_profile->'multi_qualification'->'report'),
  ('Calibration'::text,t.execution_profile->'multi_qualification'->'calibration')) refs(contract,ref)
  WHERE (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract=refs.contract)<>1
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
    WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract=refs.contract AND i.artifact_id::text=refs.ref->>'artifact_id' AND i.sha256=refs.ref->>'sha256'
     AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
     AND a.bytes BETWEEN 2 AND CASE refs.contract WHEN 'Calibration' THEN 1048576 ELSE 8388608 END
     AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp()
  AND slot.lease_until>clock_timestamp();
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_multi_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;

-- Original accepted snapshot source; object-byte and ICC verification remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_worker_multi_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;binding jsonb;auth_session uuid;operation text;frame_count integer;
BEGIN
 IF photo.v29_multi_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 body:=p_json::jsonb;
 SELECT t.operation INTO operation FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR NOT(body ?& ARRAY['schema_version','frames','reference_ordinal','max_pixels','calibration'])
 OR jsonb_typeof(body->'max_pixels') IS DISTINCT FROM 'number'
 OR coalesce(body->>'max_pixels','') !~ '^[0-9]+$'
 OR (body->>'max_pixels')::bigint NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 IF operation='optical.panorama' THEN
  IF coalesce(body->>'projection','') NOT IN ('planar','cylindrical')
  OR (body-'schema_version'-'frames'-'reference_ordinal'-'max_pixels'-'calibration'-'projection')<>'{}'::jsonb THEN RETURN false;END IF;
 ELSE
  IF coalesce(body->>'motion_policy','') NOT IN ('review','reject')
  OR (body-'schema_version'-'frames'-'reference_ordinal'-'max_pixels'-'calibration'-'motion_policy')<>'{}'::jsonb THEN RETURN false;END IF;
 END IF;
 IF jsonb_typeof(body->'frames') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 frame_count:=jsonb_array_length(body->'frames');
 IF frame_count NOT BETWEEN 2 AND CASE operation WHEN 'optical.panorama' THEN 20 ELSE 30 END
 OR jsonb_typeof(body->'reference_ordinal') IS DISTINCT FROM 'number'
 OR (body->>'reference_ordinal') !~ '^[0-9]+$'
 OR (body->>'reference_ordinal')::integer NOT BETWEEN 0 AND frame_count-1
 OR (SELECT count(DISTINCT value->>'asset_version_id') FROM jsonb_array_elements(body->'frames'))<>frame_count THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 auth_session:=(r.config_snapshot->'v29_multi'->>'auth_session_id')::uuid;
 IF w.id IS NULL OR w.workflow_type IS DISTINCT FROM 'preview'
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text

 THEN RETURN false;END IF;
 FOR binding IN SELECT value FROM jsonb_array_elements(body->'frames') LOOP
 IF jsonb_typeof(binding) IS DISTINCT FROM 'object'
 OR NOT(binding ?& ARRAY['asset_version_id','source','canvas_hash'])
 OR (binding-'asset_version_id'-'source'-'canvas_hash')<>'{}'::jsonb
 OR jsonb_typeof(binding->'source') IS DISTINCT FROM 'object'
 OR NOT(binding->'source' ?& ARRAY['artifact_id','sha256'])
 OR (binding->'source'-'artifact_id'-'sha256')<>'{}'::jsonb
 OR coalesce(binding->>'canvas_hash','') !~ '^[0-9a-f]{64}$'
 OR coalesce(binding->'source'->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,binding) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.asset_versions v
 ON v.tenant_id=i.tenant_id AND v.id=(binding->>'asset_version_id')::uuid
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='WorkingImage'
 AND i.artifact_id::text=binding->'source'->>'artifact_id'
 AND i.sha256=binding->'source'->>'sha256'
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task
 AND t.execution_profile->'multi_qualification'->'calibration'=body->'calibration') THEN RETURN false;END IF;
 RETURN photo.v29_multi_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- Leased multi report/calibration evidence; no object IO in SQL.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_worker_multi_qualification_evidence(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;p photo.v2_qualification_policies%ROWTYPE;report photo.artifacts%ROWTYPE;
 calibration photo.artifacts%ROWTYPE;c jsonb;answer jsonb;previous_tenant text;operation text;
BEGIN
 IF photo.v29_worker_multi_source_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT execution_profile->'multi_qualification',tasks.operation INTO pin,operation FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run;
 SELECT jsonb_build_object('project_id',w.project_id,'actor_id',r.created_by,
 'auth_session_id',r.config_snapshot->'v29_multi'->>'auth_session_id') INTO c
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 IF c IS NULL OR pin IS NULL THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF photo.lock_v29_multi_qualification(p_tenant,(c->>'project_id')::uuid,(c->>'actor_id')::uuid,(c->>'auth_session_id')::uuid,pin,operation) IS DISTINCT FROM true THEN
  PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 SELECT * INTO calibration FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'calibration'->>'artifact_id')::uuid;
 answer:=jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type),
 'calibration',jsonb_build_object('artifact_id',calibration.id,'sha256',calibration.sha256,'bucket',calibration.bucket,
 'object_key',calibration.object_key,'object_version',calibration.object_version,'bytes',calibration.bytes,'media_type',calibration.media_type));
 IF photo.v29_multi_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN answer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_worker_multi_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
