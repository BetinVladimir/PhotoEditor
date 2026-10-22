-- +goose Up
-- +goose StatementBegin
ALTER TABLE photo.backend_documents ADD COLUMN v30_import_evidence_ref jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_evidence_ref_shape CHECK
 (v30_import_evidence_ref IS NULL OR (purpose='task_params' AND jsonb_typeof(v30_import_evidence_ref)='object'));
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) RENAME TO complete_v30_interchange_import_document_pre_params_pin;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_import_document_pre_params_pin(uuid,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.complete_v30_interchange_import_document(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_purpose text,p_body text,p_bucket text,p_token uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE accepted boolean;
BEGIN
 accepted:=photo.complete_v30_interchange_import_document_pre_params_pin(p_tenant,p_workflow,p_parsed,p_artifact,p_purpose,p_body,p_bucket,p_token,p_version);
 IF accepted IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_purpose='task_params' THEN
  UPDATE photo.backend_documents SET v30_import_evidence_ref=p_body::jsonb->'evidence'
  WHERE tenant_id=p_tenant AND artifact_id=p_artifact AND purpose=p_purpose AND state='ready' AND token=p_token
   AND object_version=p_version AND sha256=encode(sha256(convert_to(p_body,'UTF8')),'hex');
  IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_PARAMS_EVIDENCE_PIN_REFUSED' USING ERRCODE='23514';END IF;
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_transport_scope(uuid,uuid,uuid,text) RENAME TO v30_interchange_transport_scope_pre_evidence_gate;
CREATE FUNCTION photo.v30_interchange_transport_scope(p_tenant uuid,p_run uuid,p_task uuid,p_parsed text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid;params photo.backend_documents%ROWTYPE;evidence photo.backend_documents%ROWTYPE;sources jsonb;metadata jsonb;
BEGIN
 workflow:=photo.v30_interchange_transport_scope_pre_evidence_gate(p_tenant,p_run,p_task,p_parsed);
 IF workflow IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO params FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task
 AND purpose='task_params' AND artifact_id=photo.v30_uuid5(p_task,'interchange:params:1') AND state='ready' FOR SHARE;
 SELECT * INTO evidence FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task
 AND purpose='v30_interchange_import_evidence' AND artifact_id=photo.v30_uuid5(p_task,'interchange:evidence:1') AND state='ready' FOR SHARE;
 IF params.id IS NULL OR evidence.id IS NULL OR evidence.v30_import_evidence IS NULL
 OR params.v30_import_evidence_ref IS DISTINCT FROM jsonb_build_object('artifact_id',evidence.artifact_id,'sha256',evidence.sha256) THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,workflow,p_parsed);
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,workflow,p_parsed);
 IF sources IS NULL OR metadata IS NULL OR evidence.v30_import_evidence->'sources' IS DISTINCT FROM sources
 OR evidence.v30_import_evidence->'metadata' IS DISTINCT FROM metadata THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id
 WHERE d.id IN(params.id,evidence.id) AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 -- Count separately: PERFORM's FOUND would accept only one surviving artifact.
 IF (SELECT count(*) FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id
 WHERE d.id IN(params.id,evidence.id) AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))<>2 THEN RETURN NULL;END IF;
 RETURN workflow;
END $$;
ALTER FUNCTION photo.v30_interchange_transport_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_transport_scope(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) RENAME TO seal_v30_interchange_inputs_pre_evidence_gate;
REVOKE ALL ON FUNCTION photo.seal_v30_interchange_inputs_pre_evidence_gate(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v30_interchange_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,p_count integer,p_sha text,p_roots jsonb,p_parsed text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid;params photo.backend_documents%ROWTYPE;evidence photo.backend_documents%ROWTYPE;expected jsonb;accepted boolean;
BEGIN
 workflow:=photo.v30_interchange_transport_scope(p_tenant,p_run,p_task,p_parsed);
 IF workflow IS NULL THEN RETURN false;END IF;
 SELECT * INTO params FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='task_params' AND state='ready';
 SELECT * INTO evidence FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='v30_interchange_import_evidence' AND state='ready';
 expected:=jsonb_build_array(jsonb_build_object('artifact_id',params.artifact_id,'sha256',params.sha256),
  evidence.v30_import_evidence->'plan',evidence.v30_import_evidence->'parsed',params.v30_import_evidence_ref);
 IF p_roots IS DISTINCT FROM expected THEN RETURN false;END IF;
 accepted:=photo.seal_v30_interchange_inputs_pre_evidence_gate(p_tenant,p_run,p_task,p_manifest,p_chunks,p_count,p_sha,p_roots,p_parsed);
 IF accepted IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND artifact_id=evidence.artifact_id
  AND sha256=evidence.sha256 AND contract='FrozenInterchangeImportEvidence') THEN
  RAISE EXCEPTION 'INTERCHANGE_EVIDENCE_CLOSURE_MISSING' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
