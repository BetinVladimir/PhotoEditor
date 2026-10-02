-- +goose Up
-- Immutable user protection publication binding. Producer masks are handled
-- separately; this predicate never grants project-wide fallback authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_protection_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_body jsonb,p_node jsonb,p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; raw text; mask jsonb; source jsonb; binding jsonb; root photo.v2_draft_uploads%ROWTYPE;
BEGIN
 ref:=p_body->'recipe'->'protected_mask';raw:=p_node->>'protection_jcs';binding:=p_body->'scalars'->'source';
 IF ref IS NULL OR ref='null'::jsonb THEN RETURN raw IS NULL OR raw='';END IF;
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM ref->>'sha256' THEN RETURN false;END IF;
 IF p_node->>'source_jcs' IS NULL OR octet_length(p_node->>'source_jcs') NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(p_node->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM binding->'source'->>'sha256' THEN RETURN false;END IF;
 mask:=raw::jsonb;source:=(p_node->>'source_jcs')::jsonb;
 IF jsonb_typeof(mask) IS DISTINCT FROM 'object'
  OR NOT mask ?& ARRAY['artifact','canvas','transform_chain','object_id','selector','confidence','model_lock_id','protected']
  OR mask-'artifact'-'canvas'-'transform_chain'-'object_id'-'selector'-'confidence'-'model_lock_id'-'protected'<>'{}'::jsonb
  OR mask->'protected' IS DISTINCT FROM 'true'::jsonb
  OR jsonb_typeof(mask->'selector') IS DISTINCT FROM 'string' OR length(mask->>'selector') NOT BETWEEN 1 AND 120
  OR jsonb_typeof(mask->'confidence') IS DISTINCT FROM 'number'
  OR (mask->>'confidence')::numeric NOT BETWEEN 0 AND 1
  OR mask->'canvas' IS DISTINCT FROM source->'canvas'
  OR mask->'transform_chain' IS DISTINCT FROM source->'transform_chain'
  OR jsonb_typeof(mask->'artifact') IS DISTINCT FROM 'object'
  OR (mask->'artifact')-'artifact_id'-'sha256'<>'{}'::jsonb THEN RETURN false;END IF;
 SELECT * INTO root FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant
  AND u.project_id=p_project AND u.asset_version_id=(binding->>'asset_version_id')::uuid
  AND u.artifact_id=(ref->>'artifact_id')::uuid AND u.sha256=ref->>'sha256'
  AND u.upload_contract='Document' AND u.state='ready' AND u.completed_at IS NOT NULL
  AND u.bytes=octet_length(raw)
  AND u.user_mask_binding=jsonb_build_object('source',binding->'source','canvas_hash',binding->>'canvas_hash','mask',mask);
 IF root.artifact_id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=root.artifact_id
   AND a.sha256=root.sha256 AND a.bytes=root.bytes AND a.bucket=root.bucket AND a.object_version=root.object_version
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='document'
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.read_v25_user_mask_pixels(p_tenant,(binding->>'asset_version_id')::uuid,
  p_actor,p_session,(mask->'artifact'->>'artifact_id')::uuid,mask->'artifact'->>'sha256',
  (mask->'canvas'->>'width')::bigint,(mask->'canvas'->>'height')::bigint) pixels WHERE pixels.bucket=root.bucket) THEN RETURN false;END IF;
 IF p_task IS NOT NULL THEN
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project AND i.task_id=p_task
   AND i.artifact_id=root.artifact_id AND i.sha256=root.sha256 AND i.contract='MaskRef')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project AND i.task_id=p_task
    AND i.artifact_id=(mask->'artifact'->>'artifact_id')::uuid AND i.sha256=mask->'artifact'->>'sha256' AND i.contract='NpyArray') THEN RETURN false;END IF;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
