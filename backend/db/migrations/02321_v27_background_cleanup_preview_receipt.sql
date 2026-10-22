-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v27_background_cleanup_preview_receipt jsonb
 CHECK(v27_background_cleanup_preview_receipt IS NULL OR jsonb_typeof(v27_background_cleanup_preview_receipt)='object');
-- Backend physical verifier publishes this linkage; no client/worker grant.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_background_cleanup_preview_receipt(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text,p_presentation text,p_evidence text,p_image text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;p jsonb;n jsonb;root record;sidecar record;r photo.runs%ROWTYPE;receipt jsonb;counted integer:=0;coordinate jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_presentation IS NULL OR octet_length(p_presentation) NOT BETWEEN 2 AND 1048576
 OR p_evidence IS NULL OR octet_length(p_evidence) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;p:=p_presentation::jsonb;n:=p_evidence::jsonb;
 PERFORM photo.lock_semantic_decision_publication(p_tenant,(e->>'intent_id')::uuid,p_actor,p_session,(e->>'workflow_revision')::bigint);
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=(e->>'target_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview FOR UPDATE;
 SELECT * INTO root FROM photo.semantic_background_cleanup_preview_presentation_source(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 IF root.artifact_id IS NULL OR root.bytes<>octet_length(p_presentation)
 OR root.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_presentation,'UTF8'),'sha256'),'hex') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_STALE' USING ERRCODE='40001';END IF;
 FOR sidecar IN SELECT * FROM photo.semantic_background_cleanup_preview_presentation_sidecars(p_tenant,p_preview,p_actor,p_session,p_body,p_sha,p_presentation) LOOP
 counted:=counted+1;
 IF sidecar.role='preview_evidence' AND (sidecar.bytes<>octet_length(p_evidence)
 OR sidecar.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_evidence,'UTF8'),'sha256'),'hex')) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 IF sidecar.role='working_image' AND (sidecar.bytes<>octet_length(p_image)
 OR sidecar.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_image,'UTF8'),'sha256'),'hex')) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 END LOOP;
 IF counted<>7 OR jsonb_typeof(n) IS DISTINCT FROM 'object'
 OR NOT(n ?& ARRAY['schema_version','params','working_image','pixels','algorithm','protected_pixels_unchanged','outside_mask_unchanged','changed_pixels','global_pass','roi'])
 OR (n-ARRAY['schema_version','params','working_image','pixels','algorithm','protected_pixels_unchanged','outside_mask_unchanged','changed_pixels','global_pass','roi']) IS DISTINCT FROM '{}'::jsonb
 OR n->'params' IS DISTINCT FROM e OR p->'params' IS DISTINCT FROM e
 OR n->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR n->>'algorithm' IS DISTINCT FROM 'big_lama_work_srgb_delta_v1'
 OR n->'protected_pixels_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR n->'outside_mask_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(n->'global_pass') IS DISTINCT FROM 'boolean'
 OR jsonb_typeof(n->'changed_pixels') IS DISTINCT FROM 'number'
 OR n->>'changed_pixels' !~ '^(0|[1-9][0-9]{0,8})$'
 OR (n->>'changed_pixels')::bigint>200000000
 OR jsonb_typeof(n->'roi') IS DISTINCT FROM 'array'
 OR n->'working_image' IS DISTINCT FROM p->'working_image' OR n->'pixels' IS DISTINCT FROM p->'pixels'
 OR (p_image::jsonb)->'pixels' IS DISTINCT FROM p->'pixels' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 IF jsonb_array_length(n->'roi')<>4 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 FOR coordinate IN SELECT value FROM jsonb_array_elements(n->'roi') LOOP
 IF jsonb_typeof(coordinate) IS DISTINCT FROM 'number' OR coordinate::text !~ '^(0|[1-9][0-9]{0,15})$'
 OR coordinate::text::numeric>9007199254740991 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 END LOOP;
 IF (n->'roi'->>2)::numeric<=(n->'roi'->>0)::numeric
 OR (n->'roi'->>3)::numeric<=(n->'roi'->>1)::numeric
 OR (n->>'changed_pixels')::numeric>((n->'roi'->>2)::numeric-(n->'roi'->>0)::numeric)*
 ((n->'roi'->>3)::numeric-(n->'roi'->>1)::numeric) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_INVALID' USING ERRCODE='22023';END IF;
 receipt:=jsonb_build_object('schema_version',1,'params_sha',p_sha,'task_id',root.task_id,
 'result_id',root.result_id,'attempt',root.attempt,'fence',root.fence,
 'presentation',jsonb_build_object('artifact_id',root.artifact_id,'sha256',root.sha256),
 'working_image',p->'working_image','pixels',p->'pixels','numerical_evidence',p->'numerical_evidence');
 IF r.v27_background_cleanup_preview_receipt IS NOT NULL AND r.v27_background_cleanup_preview_receipt IS DISTINCT FROM receipt THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 UPDATE photo.runs SET v27_background_cleanup_preview_receipt=receipt WHERE tenant_id=p_tenant AND id=p_preview;
 RETURN receipt;
END $$;
ALTER FUNCTION photo.seal_semantic_background_cleanup_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_background_cleanup_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_background_cleanup_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
