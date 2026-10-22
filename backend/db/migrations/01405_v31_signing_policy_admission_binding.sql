-- +goose Up
-- Require the dedicated tenant policy publication at admission and adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_policy_ready(p_tenant uuid,p_policy uuid,p_artifact uuid,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_policy IS NULL OR p_policy='00000000-0000-0000-0000-000000000000'::uuid
 OR p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_tenant,'signing:policy:'||p_policy::text||':'||p_sha) THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact FOR SHARE;
 IF NOT FOUND OR a.sha256 IS DISTINCT FROM p_sha OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL
 OR a.media_type IS DISTINCT FROM 'application/json' OR a.output_role IS DISTINCT FROM 'v31_signing_policy'
 OR a.bytes NOT BETWEEN 2 AND 1048576 OR coalesce(a.object_version,'') IN('','null')
 OR length(a.object_version)>1024 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 PERFORM d.id FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.artifact_id=a.id
 AND d.owner_id=a.id AND d.purpose='v31_signing_policy' AND d.state='ready'
 AND d.sha256=a.sha256 AND d.bytes=a.bytes AND d.bucket=a.bucket AND d.object_version=a.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||d.run_id||'/documents/'||a.id FOR SHARE;
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.v31_signing_policy_ready(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_policy_ready(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v31_signing_admission_ready(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_bucket text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;params jsonb;lineage uuid;policy uuid;source uuid;a photo.artifacts%ROWTYPE;aid uuid;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 BEGIN params:=p_params::jsonb;EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(params))<>5 THEN RETURN false;END IF;
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(params->'signing_policy_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'export') IS DISTINCT FROM 'object' OR jsonb_typeof(params->'lineage') IS DISTINCT FROM 'object'
 OR jsonb_typeof(params->'policy') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(params->'export'))<>2
 OR (SELECT count(*) FROM jsonb_object_keys(params->'lineage'))<>2
 OR (SELECT count(*) FROM jsonb_object_keys(params->'policy'))<>2
 OR jsonb_typeof(params->'export'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'export'->'sha256') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'lineage'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'lineage'->'sha256') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'policy'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'policy'->'sha256') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 BEGIN
  source:=(params->'export'->>'artifact_id')::uuid;lineage:=(params->'lineage'->>'artifact_id')::uuid;policy:=(params->'policy'->>'artifact_id')::uuid;
  IF (params->>'signing_policy_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF source IS NULL OR lineage IS NULL OR policy IS NULL OR source=lineage OR source=policy OR lineage=policy
 OR params->'export'->>'artifact_id' IS DISTINCT FROM p_expected->>'artifact'
 OR params->'export'->>'sha256' IS DISTINCT FROM p_expected->>'sha256'
 OR params->'lineage'->>'sha256' IS NULL OR params->'lineage'->>'sha256' !~ '^[0-9a-f]{64}$'
 OR params->'policy'->>'sha256' IS NULL OR params->'policy'->>'sha256' !~ '^[0-9a-f]{64}$'
 OR lineage IS DISTINCT FROM photo.v30_uuid5(p_export,'signing:lineage:'||(params->'lineage'->>'sha256')) THEN RETURN false;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);IF rid IS NULL THEN RETURN false;END IF;
 -- Stable lock order; artifact GC changes ready->deleting under conflicting locks.
 FOR aid IN SELECT id FROM unnest(ARRAY[lineage,policy]) id ORDER BY id LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid FOR SHARE;
  IF NOT FOUND OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.bucket IS DISTINCT FROM p_bucket
  OR coalesce(a.object_version,'') IN('','null') OR a.object_key='' OR a.media_type IS DISTINCT FROM 'application/json'
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
  IF aid=lineage THEN
   IF a.sha256 IS DISTINCT FROM params->'lineage'->>'sha256' OR a.bytes NOT BETWEEN 2 AND 8388608
   OR a.output_role IS DISTINCT FROM 'v31_signing_lineage' THEN RETURN false;END IF;
   PERFORM d.id FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.owner_id=p_export
   AND d.purpose='v31_signing_lineage' AND d.state='ready' AND d.artifact_id=a.id AND d.sha256=a.sha256
   AND d.bytes=a.bytes AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||a.id FOR SHARE;
   IF NOT FOUND THEN RETURN false;END IF;
  ELSE
   IF a.sha256 IS DISTINCT FROM params->'policy'->>'sha256' OR a.bytes NOT BETWEEN 2 AND 1048576
   OR photo.v31_signing_policy_ready(p_tenant,(params->>'signing_policy_id')::uuid,policy,params->'policy'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
  END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_v31_signed(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_params text,p_body text,p_policy text,p_lineage text,p_validation_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;params_ref record;
 root jsonb;params jsonb;body jsonb;policy jsonb;expected jsonb;entry jsonb;a photo.artifacts%ROWTYPE;
 selected_export uuid;signed_id uuid;manifest_id uuid;ordinal integer:=0;location text;media text;prior photo.provenance_manifests%ROWTYPE;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_policy IS NULL OR octet_length(p_policy) NOT BETWEEN 2 AND 1048576
 OR p_lineage IS NULL OR octet_length(p_lineage) NOT BETWEEN 2 AND 8388608
 OR p_validation_sha IS NULL OR p_validation_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO params_ref FROM photo.v31_signing_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF NOT FOUND OR encode(sha256(convert_to(p_params,'UTF8')),'hex') IS DISTINCT FROM params_ref.sha256 THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND workflow_type='provenance';
 SELECT outputs->0 INTO root FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence;
 IF root IS NULL OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM root->>'sha256' THEN RETURN false;END IF;
 params:=p_params::jsonb;body:=p_body::jsonb;policy:=p_policy::jsonb;
 IF body IS DISTINCT FROM jsonb_build_object('schema_version',1,'signed_export',body->'signed_export','manifest',body->'manifest')
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR encode(sha256(convert_to(p_policy,'UTF8')),'hex') IS DISTINCT FROM params->'policy'->>'sha256'
 OR encode(sha256(convert_to(p_lineage,'UTF8')),'hex') IS DISTINCT FROM params->'lineage'->>'sha256'
 OR policy->>'id' IS DISTINCT FROM params->>'signing_policy_id'
 OR policy->>'sdk_lock_sha' IS DISTINCT FROM t.execution_profile->>'sdk_lock_sha'
 OR policy->>'certificate_chain_sha' IS DISTINCT FROM t.execution_profile->>'certificate_chain_sha'
 OR policy->>'trust_bundle_sha' IS DISTINCT FROM t.execution_profile->>'trust_bundle_sha'
 OR coalesce(policy->>'key_handle','')='' OR length(policy->>'key_handle')>200 THEN RETURN false;END IF;
 selected_export:=(r.config_snapshot->>'export_id')::uuid;
 signed_id:=(body->'signed_export'->>'artifact_id')::uuid;manifest_id:=(body->'manifest'->>'artifact_id')::uuid;
 IF signed_id IS NULL OR manifest_id IS NULL OR signed_id=manifest_id
 OR signed_id=(root->>'artifact_id')::uuid OR manifest_id=(root->>'artifact_id')::uuid THEN RETURN false;END IF;
 -- Lock every immutable input object before linking new output artifacts.
 FOR entry IN SELECT value FROM jsonb_array_elements(jsonb_build_array(params->'export',params->'policy',params->'lineage')) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(entry->>'artifact_id')::uuid FOR SHARE;
  IF a.id IS NULL OR a.sha256 IS DISTINCT FROM entry->>'sha256' OR a.state<>'ready' OR a.verified_at IS NULL
   OR coalesce(a.object_version,'') IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
   OR a.id IN(signed_id,manifest_id,(root->>'artifact_id')::uuid)
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=w.project_id AND i.artifact_id=a.id AND i.sha256=a.sha256) THEN RETURN false;END IF;
 END LOOP;
 IF photo.v31_signing_policy_ready(p_tenant,(params->>'signing_policy_id')::uuid,(params->'policy'->>'artifact_id')::uuid,params->'policy'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT a.media_type INTO media FROM photo.exports e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id
 WHERE e.tenant_id=p_tenant AND e.id=selected_export AND e.status='ready' AND asset.project_id=w.project_id
 AND e.artifact_id=(params->'export'->>'artifact_id')::uuid AND a.sha256=params->'export'->>'sha256'
 AND a.media_type IN('image/jpeg','image/png') FOR SHARE OF e,v,asset,a;
 IF media IS NULL OR photo.v31_signer_worker_matches(p_tenant,t.worker_id,t.operation,t.execution_profile) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:='[]'::jsonb;
 FOR entry IN SELECT value FROM jsonb_array_elements(jsonb_build_array(root,body->'signed_export',body->'manifest')) LOOP
  location:=CASE ordinal WHEN 0 THEN '$' WHEN 1 THEN '$/signed_export' ELSE '$/manifest' END;
  expected:=expected||jsonb_build_array(jsonb_build_object('artifact_id',entry->>'artifact_id','sha256',entry->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/'||location,'UTF8')),'hex')));
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(entry->>'artifact_id')::uuid FOR SHARE;
  IF a.id IS NULL OR a.sha256 IS DISTINCT FROM entry->>'sha256' OR a.created_by_task_id IS DISTINCT FROM p_task
   OR a.produced_attempt IS DISTINCT FROM p_attempt OR a.produced_fence IS DISTINCT FROM p_fence
   OR a.output_role IS DISTINCT FROM (CASE ordinal WHEN 0 THEN 'document' WHEN 1 THEN 'signed_export' ELSE 'manifest' END)
   OR a.media_type IS DISTINCT FROM (CASE ordinal WHEN 0 THEN 'application/json' WHEN 1 THEN media ELSE 'application/c2pa' END)
   OR a.bytes NOT BETWEEN 1 AND (CASE ordinal WHEN 1 THEN 536870912 ELSE 8388608 END) THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 IF p_inventory IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT pm.* INTO prior FROM photo.provenance_manifests pm WHERE pm.tenant_id=p_tenant AND pm.export_id=selected_export AND pm.signing_policy_sha=params->'policy'->>'sha256';
 IF FOUND THEN
  IF prior.workflow_id IS DISTINCT FROM w.id OR prior.signed_artifact_id IS DISTINCT FROM signed_id OR prior.manifest_artifact_id IS DISTINCT FROM manifest_id THEN
   RAISE EXCEPTION 'SIGNING_PROVENANCE_CONFLICT' USING ERRCODE='23514';END IF;
  RETURN false;
 END IF;
 INSERT INTO photo.provenance_manifests(tenant_id,project_id,export_id,workflow_id,manifest_artifact_id,signed_artifact_id,signer_key_id,signing_policy_sha)
 VALUES(p_tenant,w.project_id,selected_export,w.id,manifest_id,signed_id,policy->>'key_handle',params->'policy'->>'sha256');
 IF photo.adopt_v2_plan_pre_v31_signer(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'SIGNING_ADOPTION_STALE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v31_signed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v31_signed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v31_signed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
