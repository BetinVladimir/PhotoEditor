-- +goose Up
-- Accepted command recovery needs immutable receipt identity, not retained
-- producer Params, source bytes or current model qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.ordinary_parent_accepted_receipt_locator(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE original photo.runs%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.idempotency_records%ROWTYPE;
 marker jsonb;recovery jsonb;request_key text;request_sha text;expected_workflow uuid;scene_original boolean:=false;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'ORDINARY_RECEIPT_ACCESS_DENIED' USING ERRCODE='42501';END IF;
 -- Current ordinary scene commands persist their immutable key/policy in the
 -- original detector. Legacy ordinary commands persisted it in the person
 -- preparation. Neither locator requires that producer's retained objects.
 SELECT * INTO original FROM photo.runs WHERE tenant_id=p_tenant
 AND id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference');
 IF original.id IS NOT NULL AND original.config_snapshot->'v27_detector_prepare'->'recovery' ? 'scene_policy' THEN
  scene_original:=true;marker:=original.config_snapshot->'v27_detector_prepare';
 ELSE
  SELECT * INTO original FROM photo.runs WHERE tenant_id=p_tenant
  AND id=photo.semantic_uuid_v5(p_parent,'semantic.mask.person:reference');
  marker:=original.config_snapshot->'v27_person_prepare';
 END IF;
 IF original.id IS NULL THEN RETURN NULL;END IF;
 recovery:=marker->'recovery';
 IF original.kind<>'v2' OR original.created_by IS DISTINCT FROM p_actor
 OR original.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR marker->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR marker->>'parent_run_id' IS DISTINCT FROM p_parent::text
 OR marker->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR (NOT scene_original AND (recovery->>'continuation' IS DISTINCT FROM 'ordinary_parent'
 OR recovery->'ordinary_policy'->>'project_id' IS DISTINCT FROM p_project::text))
 OR (scene_original AND (jsonb_typeof(recovery->'scene_policy') IS DISTINCT FROM 'object'
 OR jsonb_typeof(recovery->'scene_policy'->'policy') IS DISTINCT FROM 'object'
 OR jsonb_typeof(recovery->'scene_policy'->'scene') IS DISTINCT FROM 'object'
 OR jsonb_typeof(recovery->'scene_policy'->'depth') IS DISTINCT FROM 'object'))
 OR jsonb_typeof(recovery->'request_key') IS DISTINCT FROM 'string'
 OR jsonb_typeof(marker->'request_sha') IS DISTINCT FROM 'string'
 OR original.config_hash IS DISTINCT FROM encode(public.digest(convert_to(photo.v27_target_manifest_json_text(original.config_snapshot),'UTF8'),'sha256'),'hex') THEN
 RAISE EXCEPTION 'ORDINARY_RECEIPT_BINDING_INVALID' USING ERRCODE='23514';END IF;
 request_key:=recovery->>'request_key';request_sha:=marker->>'request_sha';
 IF length(request_key) NOT BETWEEN 1 AND 128 OR request_key !~ '^[!-~]+$' OR request_sha !~ '^[0-9a-f]{64}$'
 OR photo.semantic_uuid_v5(p_tenant,'V2.Admit:'||p_actor::text||':'||request_key) IS DISTINCT FROM p_parent THEN
 RAISE EXCEPTION 'ORDINARY_RECEIPT_IDENTITY_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='V2.Admit' AND key=request_key;
 IF i.response_status IS NULL THEN RETURN NULL;END IF;
 IF i.request_hash IS DISTINCT FROM request_sha OR i.response_status<>202 OR i.resource_id IS DISTINCT FROM p_parent THEN
 RAISE EXCEPTION 'ORDINARY_RECEIPT_LEDGER_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_parent;
 expected_workflow:=photo.semantic_uuid_v5(p_parent,'workflow');
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.created_by IS DISTINCT FROM p_actor
 OR w.id IS DISTINCT FROM expected_workflow OR w.project_id IS DISTINCT FROM p_project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2'
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR r.config_snapshot->'v27_semantic'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR i.response_body IS DISTINCT FROM jsonb_build_object('run_id',p_parent,'workflow_id',expected_workflow,'state','queued') THEN
 RAISE EXCEPTION 'ORDINARY_RECEIPT_PARENT_INVALID' USING ERRCODE='23514';END IF;
 RETURN jsonb_build_object('key',request_key,'request_sha',request_sha,'run_id',p_parent,'workflow_id',expected_workflow);
END $$;
ALTER FUNCTION photo.ordinary_parent_accepted_receipt_locator(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ordinary_parent_accepted_receipt_locator(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
DO $acl$ DECLARE x record;BEGIN
 FOR x IN SELECT a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid='photo.ordinary_parent_accepted_receipt_locator(uuid,uuid,uuid,uuid,uuid)'::regprocedure AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.ordinary_parent_accepted_receipt_locator(uuid,uuid,uuid,uuid,uuid) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=x.grantee));END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.ordinary_parent_accepted_receipt_locator(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
