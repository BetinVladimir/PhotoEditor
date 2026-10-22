-- +goose Up
-- Background cleanup presentation and sidecars are scoped to one canonical task attempt.
-- Authenticated metadata for the single canonical presentation output.
-- Raw presentation still requires exact byte/hash/schema checks after object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_presentation_source(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,artifact_id uuid,
 sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;owner uuid;
BEGIN
 PERFORM * FROM photo.inspect_semantic_background_cleanup_preview(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 e:=p_body::jsonb;
 owner:=photo.semantic_uuid_v5(p_preview,'semantic.preview_background_cleanup:'||(e->>'target_id')::uuid::text);
 RETURN QUERY SELECT t.id,z.id,t.attempt,t.fencing_token,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=owner
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
 AND a.sha256=z.outputs->0->>'sha256'
 WHERE r.tenant_id=p_tenant AND r.id=p_preview AND r.kind='preview'
 AND r.status IN('running','completed') AND r.graph_sealed AND NOT r.cancel_requested
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.operation='semantic.preview_background_cleanup'
 AND t.operation_version='1' AND t.resource_class='gpu'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND a.output_role='preview_presentation' AND a.media_type='application/json' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.semantic_background_cleanup_preview_closure_ready(t.id) IS TRUE;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_presentation_source(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_presentation_source(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_background_cleanup_preview_presentation_source(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd

-- Presentation-bound sidecars; raw bytes must match the canonical presentation.
-- All sidecars must originate from the same canonical task attempt/fence.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_presentation_sidecars(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text,p_presentation_body text)
RETURNS TABLE(role text,artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE evidence record;e jsonb;item record;a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO evidence FROM photo.semantic_background_cleanup_preview_presentation_source(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 IF evidence.artifact_id IS NULL THEN RETURN;END IF;
 IF p_presentation_body IS NULL OR octet_length(p_presentation_body) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_presentation_body) IS DISTINCT FROM evidence.bytes
 OR encode(public.digest(convert_to(p_presentation_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM evidence.sha256 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PRESENTATION_INVALID' USING ERRCODE='22023';END IF;
 e:=p_presentation_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR NOT(e ?& ARRAY['schema_version','params','numerical_evidence','working_image','pixels','before','after','affected','protected','width','height','icc_lock_sha','srgb_sha','resize','overlay'])
 OR (e-ARRAY['schema_version','params','numerical_evidence','working_image','pixels','before','after','affected','protected','width','height','icc_lock_sha','srgb_sha','resize','overlay']) IS DISTINCT FROM '{}'::jsonb
 OR jsonb_typeof(e->'icc_lock_sha') IS DISTINCT FROM 'string'
 OR jsonb_typeof(e->'srgb_sha') IS DISTINCT FROM 'string'
 OR e->'params' IS DISTINCT FROM p_body::jsonb
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'resize' IS DISTINCT FROM 'area_linear_premult_v1'
 OR e->>'overlay' IS DISTINCT FROM 'max_coverage_ceil_alpha_v1'
 OR jsonb_typeof(e->'width') IS DISTINCT FROM 'number'
 OR jsonb_typeof(e->'height') IS DISTINCT FROM 'number'
 OR e->>'width' !~ '^[1-9][0-9]{0,3}$' OR e->>'height' !~ '^[1-9][0-9]{0,3}$'
 OR (e->>'width')::integer>2560 OR (e->>'height')::integer>2560
 OR e->>'icc_lock_sha' IS NULL OR e->>'icc_lock_sha' !~ '^[0-9a-f]{64}$'
 OR e->>'srgb_sha' IS NULL OR e->>'srgb_sha' !~ '^[0-9a-f]{64}$'
 OR NOT(e ?& ARRAY['numerical_evidence','working_image','pixels','before','after','affected','protected'])
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_each(e)
     WHERE key IN('numerical_evidence','working_image','pixels','before','after','affected','protected'))<>7 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PRESENTATION_INVALID' USING ERRCODE='22023';END IF;
 FOR item IN SELECT * FROM (VALUES('working_image',e->'working_image','application/json',1048576::bigint),
 ('pixels',e->'pixels','application/x-npy',2400016384::bigint),
 ('preview_evidence',e->'numerical_evidence','application/json',1048576::bigint),
 ('before',e->'before','image/png',33882112::bigint),
 ('after',e->'after','image/png',33882112::bigint),
 ('affected',e->'affected','image/png',32833536::bigint),
 ('protected',e->'protected','image/png',32833536::bigint)) AS outputs(role,ref,media,maximum) LOOP
 IF jsonb_typeof(item.ref) IS DISTINCT FROM 'object'
 OR NOT(item.ref ?& ARRAY['artifact_id','sha256'])
 OR (item.ref-'artifact_id'-'sha256') IS DISTINCT FROM '{}'::jsonb
 OR jsonb_typeof(item.ref->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(item.ref->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(item.ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR coalesce(item.ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_PRESENTATION_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO a FROM photo.artifacts object WHERE object.tenant_id=p_tenant
 AND object.id=(item.ref->>'artifact_id')::uuid AND object.sha256=item.ref->>'sha256';
 IF a.id IS NULL OR a.id=evidence.artifact_id OR a.created_by_task_id IS DISTINCT FROM evidence.task_id
 OR a.produced_attempt IS DISTINCT FROM evidence.attempt OR a.produced_fence IS DISTINCT FROM evidence.fence
 OR a.output_role IS DISTINCT FROM item.role OR a.media_type IS DISTINCT FROM item.media
 OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL
 OR a.bytes IS NULL OR a.bytes NOT BETWEEN 2 AND item.maximum
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_OUTPUT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 role:=item.role;artifact_id:=a.id;sha256:=a.sha256;bucket:=a.bucket;object_key:=a.object_key;
 object_version:=a.object_version;bytes:=a.bytes;media_type:=a.media_type;RETURN NEXT;
 END LOOP;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_background_cleanup_preview_presentation_sidecars(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
