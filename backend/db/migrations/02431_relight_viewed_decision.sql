-- +goose Up
-- HMAC verification belongs to the API. This private gate independently checks
-- its verified grant against locked live SQL evidence and uses wall-clock time.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_viewed_pair_ready(p_tenant uuid,p_review uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_grant jsonb) RETURNS boolean
 LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;pin jsonb;image photo.artifacts%ROWTYPE;
 producer photo.tasks%ROWTYPE;presentation photo.runs%ROWTYPE;result photo.task_results%ROWTYPE;
 source_id uuid;source_sha text;receipt jsonb;srgb text;profile text;manifest photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_revision IS DISTINCT FROM 1 OR jsonb_typeof(p_grant) IS DISTINCT FROM 'object'
 OR (p_grant-ARRAY['version','kind','revision','before','after','expires_unix'])<>'{}'::jsonb
 OR p_grant->>'version' IS DISTINCT FROM '1'
 OR p_grant->>'kind' IS DISTINCT FROM 'image.relight'
 OR p_grant->>'revision' IS DISTINCT FROM p_revision::text
 OR coalesce(p_grant->>'expires_unix','') !~ '^[0-9]{1,12}$' THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR SHARE;
 IF NOT FOUND OR v.kind<>'image.relight' OR v.revision NOT IN(p_revision,p_revision+1)
 OR p_revision<>1 THEN RETURN false;END IF;
 -- Lock the selected presentation and both producer runs/tasks before artifacts.
 SELECT r.config_snapshot->>'srgb_sha256' INTO srgb FROM photo.runs r
  WHERE r.tenant_id=p_tenant AND r.v26_derivative_task_id=v.task_id
   AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested FOR SHARE;
 IF coalesce(srgb,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(jsonb_build_array(p_grant->'before',p_grant->'after')) LOOP
  IF jsonb_typeof(pin) IS DISTINCT FROM 'object'
  OR (pin-ARRAY['version','tenant','review','user','session','side','variant','source','source_sha256','role','artifact','sha256','object_key_sha256','object_version','expires_unix'])<>'{}'::jsonb
  OR coalesce(pin->>'artifact','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(pin->>'expires_unix','') !~ '^[0-9]{1,12}$'
  OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR coalesce(pin->>'object_key_sha256','') !~ '^[0-9a-f]{64}$'
  OR coalesce(pin->>'object_version','') IN('','null')
  OR pin->>'tenant' IS DISTINCT FROM p_tenant::text
  OR pin->>'review' IS DISTINCT FROM p_review::text
  OR pin->>'user' IS DISTINCT FROM p_actor::text
  OR pin->>'session' IS DISTINCT FROM p_session::text
  OR pin->>'role' IS DISTINCT FROM 'image' OR pin->>'version' IS DISTINCT FROM '1'
  OR coalesce(pin->>'variant','') NOT IN('preview1600','preview2560')
  OR coalesce(pin->>'side','') NOT IN('before','after') THEN RETURN false;END IF;
  profile:=encode(sha256(convert_to(format(
   '{"icc_sha256":"%s","long_edge":%s,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"%s"}',
   srgb,CASE pin->>'variant' WHEN 'preview1600' THEN 1600 ELSE 2560 END,pin->>'variant'),'UTF8')),'hex');
  PERFORM r.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
   WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact')::uuid FOR SHARE OF r;
  PERFORM t.id FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
   WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact')::uuid FOR SHARE OF t;
  source_id:=CASE pin->>'side' WHEN 'before' THEN v.base_artifact_id ELSE v.candidate_artifact_id END;
  SELECT sha256 INTO source_sha FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source_id FOR SHARE;
  IF pin->>'source' IS DISTINCT FROM source_id::text OR pin->>'source_sha256' IS DISTINCT FROM source_sha THEN RETURN false;END IF;
  SELECT * INTO image FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact')::uuid FOR SHARE;
  IF NOT FOUND OR image.sha256 IS DISTINCT FROM pin->>'sha256'
  OR image.object_version IS DISTINCT FROM pin->>'object_version'
  OR encode(sha256(convert_to(image.object_key,'UTF8')),'hex') IS DISTINCT FROM pin->>'object_key_sha256'
  OR image.state<>'ready' OR image.verified_at IS NULL
  OR image.output_role<>'derivative' OR image.bytes NOT BETWEEN 2 AND 54525952
  OR image.media_type NOT IN('image/jpeg','image/webp') THEN RETURN false;END IF;
  SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=image.created_by_task_id FOR SHARE;
  IF NOT FOUND OR producer.operation<>'media.preview' OR producer.state<>'succeeded'
  OR producer.operation_version<>'1' OR producer.cancel_requested OR image.produced_attempt<>producer.attempt
  OR image.produced_fence<>producer.fencing_token THEN RETURN false;END IF;
  SELECT * INTO presentation FROM photo.runs WHERE tenant_id=p_tenant AND id=producer.run_id FOR SHARE;
  IF NOT FOUND OR presentation.status<>'completed' OR presentation.cancel_requested THEN RETURN false;END IF;
  SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant
   AND id=producer.canonical_result_id AND task_id=producer.id FOR SHARE;
  IF NOT FOUND OR result.attempt<>producer.attempt OR result.fencing_token<>producer.fencing_token
  OR NOT result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',image.id::text,'sha256',image.sha256))
  THEN RETURN false;END IF;
  PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND artifact_id=image.id AND task_id=producer.id FOR SHARE;
  IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant
   AND u.artifact_id=image.id AND u.task_id=producer.id AND u.state='ready'
   AND u.attempt=producer.attempt AND u.fence=producer.fencing_token
   AND u.object_version=image.object_version AND u.bucket=image.bucket
   AND u.object_key=image.object_key AND u.sha256=image.sha256 AND u.bytes=image.bytes
   AND u.media_type=image.media_type AND u.role=image.output_role) THEN RETURN false;END IF;
  -- Render-derived pair: source and image must be in the adopted receipt.
  SELECT d.v26_derivative_receipt INTO receipt FROM photo.backend_documents d
   WHERE d.tenant_id=p_tenant AND d.owner_id=producer.id AND d.run_id=presentation.id
    AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=producer.params_artifact_id FOR SHARE;
  IF presentation.kind='preview' AND presentation.v26_derivative_task_id=v.task_id
   AND producer.node_key='render.derivative:'||(pin->>'variant') THEN
   IF receipt->>'result_id' IS DISTINCT FROM result.id::text
    OR receipt->>'attempt' IS DISTINCT FROM producer.attempt::text
    OR receipt->>'fence' IS DISTINCT FROM producer.fencing_token::text
    OR receipt->'manifest'->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',source_id::text,'sha256',source_sha)
    OR receipt->'manifest'->>'variant' IS DISTINCT FROM pin->>'variant'
    OR receipt->'manifest'->>'profile_hash' IS DISTINCT FROM profile
    OR presentation.config_snapshot->>'source_result_id' IS DISTINCT FROM (SELECT canonical_result_id::text FROM photo.tasks WHERE tenant_id=p_tenant AND id=v.task_id)
    OR receipt->'manifest'->'derivative' IS DISTINCT FROM jsonb_build_object('artifact_id',image.id::text,'sha256',image.sha256)
    THEN RETURN false;END IF;
  ELSE
   -- Only the before side may use the accepted asset's media-preview fallback.
   IF pin->>'side'<>'before' OR NOT producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',source_id::text,'sha256',source_sha)) THEN RETURN false;END IF;
   PERFORM d.artifact_id FROM photo.derivatives d WHERE d.tenant_id=p_tenant
    AND d.asset_version_id=(v.relight_params_jcs::jsonb->'base'->>'asset_version_id')::uuid
    AND d.variant=pin->>'variant' AND d.profile_hash=profile FOR SHARE;
   IF NOT EXISTS(SELECT 1 FROM photo.derivatives d WHERE d.tenant_id=p_tenant
    AND d.asset_version_id=(v.relight_params_jcs::jsonb->'base'->>'asset_version_id')::uuid
    AND d.variant=pin->>'variant' AND d.profile_hash=profile AND d.status='ready' AND d.artifact_id=image.id
    AND d.task_id=producer.id) THEN RETURN false;END IF;
  END IF;
  SELECT * INTO manifest FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.created_by_task_id=producer.id AND a.output_role='derivative_manifest' FOR SHARE;
  IF NOT FOUND OR manifest.state<>'ready' OR manifest.verified_at IS NULL
   OR manifest.media_type<>'application/json' OR manifest.object_version IS NULL
   OR manifest.object_version IN('','null') OR manifest.bytes NOT BETWEEN 2 AND 1048576
   OR manifest.produced_attempt IS DISTINCT FROM producer.attempt
   OR manifest.produced_fence IS DISTINCT FROM producer.fencing_token
   OR manifest.bucket IS DISTINCT FROM image.bucket
   OR jsonb_array_length(result.outputs)<>2
   OR NOT result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',manifest.id::text,'sha256',manifest.sha256))
   THEN RETURN false;END IF;
  PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND artifact_id=manifest.id AND task_id=producer.id FOR SHARE;
  IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant
   AND u.artifact_id=manifest.id AND u.task_id=producer.id AND u.state='ready'
   AND u.attempt=producer.attempt AND u.fence=producer.fencing_token
   AND u.object_version=manifest.object_version AND u.bucket=manifest.bucket
   AND u.object_key=manifest.object_key AND u.sha256=manifest.sha256 AND u.bytes=manifest.bytes
   AND u.media_type=manifest.media_type AND u.role=manifest.output_role)
   OR manifest.expires_at<=clock_timestamp() THEN RETURN false;END IF;
  IF image.expires_at<=clock_timestamp() OR (pin->>'expires_unix')::bigint<=extract(epoch FROM clock_timestamp())
   OR (p_grant->>'expires_unix')::bigint>(pin->>'expires_unix')::bigint THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND (a.id IN((p_grant->'before'->>'artifact')::uuid,(p_grant->'after'->>'artifact')::uuid)
   OR (a.output_role='derivative_manifest' AND a.created_by_task_id IN(SELECT created_by_task_id FROM photo.artifacts
    WHERE tenant_id=p_tenant AND id IN((p_grant->'before'->>'artifact')::uuid,(p_grant->'after'->>'artifact')::uuid))))
  AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 RETURN (p_grant->'before'->>'expires_unix')::bigint>extract(epoch FROM clock_timestamp())
  AND (p_grant->'after'->>'expires_unix')::bigint>extract(epoch FROM clock_timestamp())
  AND p_grant->'before'->>'side'='before'  AND p_grant->'after'->>'side'='after'
  AND p_grant->'before'->>'variant'=p_grant->'after'->>'variant'
  AND (p_grant->>'expires_unix')::bigint>extract(epoch FROM clock_timestamp());
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb) FROM PUBLIC;
ALTER FUNCTION photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 RENAME TO decide_v28_relight_review_pre_viewed;
REVOKE ALL ON FUNCTION photo.decide_v28_relight_review_pre_viewed(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text) FROM PUBLIC,photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.decide_v28_relight_review(p_tenant uuid,p_review uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_decision text,p_selection jsonb,
 p_hash text,p_route text,p_grant jsonb) RETURNS boolean LANGUAGE plpgsql
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v28_relight_review_decision_ready(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true
 OR photo.v28_relight_viewed_pair_ready(p_tenant,p_review,p_actor,p_session,p_revision,p_grant) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF photo.decide_v28_relight_review_pre_viewed(p_tenant,p_review,p_actor,p_session,p_revision,p_decision,p_selection,p_hash,p_route) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- A failure here must roll back the preceding UPDATE, never return false
 -- after a mutation that the caller could accidentally commit.
 IF photo.v28_relight_review_authority(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true
 OR photo.v28_relight_viewed_pair_ready(p_tenant,p_review,p_actor,p_session,p_revision,p_grant) IS DISTINCT FROM true
 THEN RAISE EXCEPTION 'RELIGHT_VIEWED_RECEIPT_STALE' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text,jsonb) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
