-- +goose Up
-- A single historical render input object, registered to an adopted ancestor
-- beneath the active Delivery delegation. Typed parent-child semantics and
-- exact bytes are checked by the closure walker after this lookup.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_input_object(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_artifact uuid,p_sha text,p_contract text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 IF p_sha!~'^[0-9a-f]{64}$' OR length(p_contract) NOT BETWEEN 1 AND 120
  OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN NULL;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'key',a.object_key,
  'version',a.object_version,'sha256',a.sha256,'bytes',a.bytes,
  'media_type',a.media_type)
 INTO result FROM photo.v2_task_inputs i
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256
 JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id
  AND w.project_id=i.project_id AND w.run_id=(e->>'run')::uuid
 WHERE i.tenant_id=(source->>'tenant_id')::uuid AND i.task_id=p_task
  AND i.project_id=(source->>'project_id')::uuid
  AND i.artifact_id=p_artifact AND i.sha256=p_sha
  AND i.contract=p_contract AND i.ordinal<1000000
  AND i.contract NOT IN('InputClosureChunk','InputClosureManifest')
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
  AND a.bucket=p_bucket AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 1 AND 68719476736
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_input_object(uuid,uuid,integer,uuid,uuid,uuid,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_input_object(uuid,uuid,integer,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_input_object(uuid,uuid,integer,uuid,uuid,uuid,text,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
