-- +goose Up
-- Dedicated backend-authored lineage storage. This is publication authority,
-- not proof of operation claims or C2PA qualification. Never use task_params.
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v31_signing_lineage'')',expression);
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_bytes_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_BYTES_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_bytes_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_bytes_check CHECK ((%s) OR (purpose=''v31_signing_lineage'' AND bytes BETWEEN 2 AND 8388608))',expression);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_lineage_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_bucket text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pins jsonb;rid uuid;pid uuid;
BEGIN
 IF p_expected IS NULL OR jsonb_typeof(p_expected)<>'object' THEN RETURN NULL;END IF;
 SELECT t.run_id,asset.project_id,jsonb_build_object('project',asset.project_id,'version',v.id,'task',t.id,'run',t.run_id,
  'artifact',a.id,'sha256',a.sha256,'key',a.object_key,'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)
 INTO rid,pid,pins
 FROM photo.exports e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.projects p ON p.tenant_id=asset.tenant_id AND p.id=asset.project_id
 JOIN photo.tenants tenant_row ON tenant_row.id=e.tenant_id
 JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.id=e.task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id
 WHERE e.tenant_id=p_tenant AND e.id=p_export AND e.status='ready' AND asset.deleted_at IS NULL
 AND p.archived_at IS NULL AND p.download_allowed AND tenant_row.status='active'
 AND t.state='succeeded' AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN ('','null')
 AND a.bucket=p_bucket AND a.object_key<>'' AND a.bytes BETWEEN 1 AND 536870912 AND a.media_type IN ('image/jpeg','image/png')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF e,v,asset,p,tenant_row,t,z,a;
 IF NOT FOUND OR pins IS DISTINCT FROM p_expected OR NOT photo.v2_editor_authorized(p_tenant,pid,p_actor,p_session) THEN RETURN NULL;END IF;
 RETURN rid;
END $$;
ALTER FUNCTION photo.v31_signing_lineage_scope(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_lineage_scope(uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v31_signing_lineage(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;d photo.backend_documents%ROWTYPE;fence uuid:=gen_random_uuid();capacity numeric;used numeric;
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_export IS NULL OR p_artifact IS NULL
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_export,'signing:lineage:'||p_sha)
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 -- Usage first: same order as pending-document cleanup; it cannot claim an
 -- expired reservation concurrently with renewal or completion.
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);
 IF rid IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=p_export AND b.purpose='v31_signing_lineage' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
   AND a.state='ready' AND a.bucket=d.bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id
   AND a.media_type='application/json' AND a.output_role='v31_signing_lineage' AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
   IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence,cleanup_after=clock_timestamp()+interval '3 hours' WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 IF used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,rid,p_export,'v31_signing_lineage',p_artifact,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v31_signing_lineage(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text,p_token uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_export IS NULL OR p_artifact IS NULL
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_export,'signing:lineage:'||p_sha)
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);IF rid IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=p_export AND b.purpose='v31_signing_lineage' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha
 OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN
  IF d.object_version IS DISTINCT FROM p_version THEN RETURN false;END IF;
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id
  AND a.media_type='application/json' AND a.output_role='v31_signing_lineage' AND a.object_version=p_version AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  RETURN FOUND;
 END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),'v31_signing_lineage');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text,uuid,text) TO photo_api;
-- +goose StatementEnd

-- Preserve every existing GC reference branch; retain lineage while its ready export exists.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT
  EXISTS(SELECT FROM photo.backend_documents d JOIN photo.exports e ON e.tenant_id=d.tenant_id AND e.id=d.owner_id
   WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose='v31_signing_lineage' AND d.state='ready' AND e.status='ready')
  OR
  EXISTS(SELECT FROM photo.provenance_manifests m WHERE m.tenant_id=p_tenant AND p_artifact IN(m.manifest_artifact_id,m.signed_artifact_id))
  OR EXISTS(SELECT FROM photo.provenance_manifests m JOIN photo.exports e ON e.tenant_id=m.tenant_id AND e.id=m.export_id WHERE m.tenant_id=p_tenant AND e.artifact_id=p_artifact)
  OR
  EXISTS(SELECT FROM photo.delivery_packages d WHERE d.tenant_id=p_tenant AND p_artifact IN(d.policy_artifact_id,d.manifest_artifact_id))
  OR EXISTS(SELECT FROM photo.delivery_items d JOIN photo.exports e ON e.tenant_id=d.tenant_id AND e.id=d.export_id WHERE d.tenant_id=p_tenant AND e.artifact_id=p_artifact)
  OR
  EXISTS(SELECT FROM photo.v2_documents x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_document_refs x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_artifact_links x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_task_inputs x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_document_approvals x WHERE x.tenant_id=p_tenant AND x.resolved_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_review_batches x WHERE x.tenant_id=p_tenant AND x.decisions_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.model_qualifications x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.session_analyses x WHERE x.tenant_id=p_tenant AND x.plan_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.person_observations x WHERE x.tenant_id=p_tenant AND x.evidence_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.learning_versions x WHERE x.tenant_id=p_tenant AND p_artifact IN(x.training_artifact_id,x.model_artifact_id,x.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact AND x.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant AND (x.policy->'calibration'->>'artifact_id'=p_artifact::text OR x.capture_policy->'calibration'->>'artifact_id'=p_artifact::text))
  OR EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
   JOIN photo.artifacts root ON root.tenant_id=u.tenant_id AND root.id=u.artifact_id
   WHERE u.tenant_id=p_tenant AND u.user_mask_binding IS NOT NULL
    AND u.state IN('ready','retired') AND root.state='ready'
    AND (u.user_mask_binding->'source'->>'artifact_id'=p_artifact::text
      OR u.user_mask_binding->'mask'->'artifact'->>'artifact_id'=p_artifact::text)
    AND (  EXISTS(SELECT FROM photo.v2_documents r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_document_refs r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_artifact_links r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_task_inputs r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_document_approvals r WHERE r.tenant_id=p_tenant AND r.resolved_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_review_batches r WHERE r.tenant_id=p_tenant AND r.decisions_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.model_qualifications r WHERE r.tenant_id=p_tenant AND r.report_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.session_analyses r WHERE r.tenant_id=p_tenant AND r.plan_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.person_observations r WHERE r.tenant_id=p_tenant AND r.evidence_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.learning_versions r WHERE r.tenant_id=p_tenant AND u.artifact_id IN(r.training_artifact_id,r.model_artifact_id,r.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id AND r.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts r WHERE r.tenant_id=p_tenant AND r.report_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.shoot_sessions r WHERE r.tenant_id=p_tenant AND (r.policy->'calibration'->>'artifact_id'=u.artifact_id::text OR r.capture_policy->'calibration'->>'artifact_id'=u.artifact_id::text))))

  OR EXISTS(SELECT 1 FROM photo.semantic_native_protections n
   JOIN photo.artifacts root ON root.tenant_id=n.tenant_id AND root.id=n.root_id
   WHERE n.tenant_id=p_tenant AND root.state='ready'
    AND p_artifact IN(n.pixels_id,n.report_id,n.source_id)
    AND (EXISTS(SELECT FROM photo.v2_documents x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_document_refs x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_artifact_links x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_task_inputs x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_document_approvals x WHERE x.tenant_id=p_tenant AND x.resolved_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_review_batches x WHERE x.tenant_id=p_tenant AND x.decisions_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.model_qualifications x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.session_analyses x WHERE x.tenant_id=p_tenant AND x.plan_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.person_observations x WHERE x.tenant_id=p_tenant AND x.evidence_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.learning_versions x WHERE x.tenant_id=p_tenant AND n.root_id IN(x.training_artifact_id,x.model_artifact_id,x.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id AND x.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant AND (x.policy->'calibration'->>'artifact_id'=n.root_id::text OR x.capture_policy->'calibration'->>'artifact_id'=n.root_id::text))))

$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_artifact_has_references(uuid,uuid) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
