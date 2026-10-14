-- +goose Up
-- Locate only pixels/alpha named by the physically read selected WorkingImage.
-- The source document is checked against the decided route on every call.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_restoration_selected_array(
 p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,
 p_task uuid,p_run uuid,p_hash text,p_route text,p_image text,
 p_channel text,p_artifact uuid,p_sha text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected jsonb;image jsonb;ref jsonb;r photo.runs%ROWTYPE;
 t photo.tasks%ROWTYPE;a photo.artifacts%ROWTYPE;result_id uuid;root_id uuid;
 root_sha text;role text;locator jsonb;source_version uuid;
BEGIN
 selected:=photo.read_v28_restoration_selected_source(p_tenant,p_project,p_actor,
  p_session,p_review,p_task,p_run,p_hash,p_route);
 IF selected IS NULL OR p_route='skip' OR p_image IS NULL
  OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(p_image,'UTF8')),'hex')
   IS DISTINCT FROM selected->'source'->>'sha256'
  OR p_channel NOT IN('pixels','alpha') OR p_artifact IS NULL
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_bucket IS DISTINCT FROM selected->'source'->>'bucket'
 THEN RETURN NULL;END IF;
 image:=p_image::jsonb;
 ref:=image->p_channel;
 IF jsonb_typeof(ref)<>'object' OR NOT ref ?& ARRAY['artifact_id','sha256']
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
  OR ref IS DISTINCT FROM jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)
 THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND run_id=p_run AND operation IN('portrait.glare','image.reflection')
  AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_adoption_receipt->>'task' IS DISTINCT FROM p_task::text
 THEN RETURN NULL;END IF;
 result_id:=t.canonical_result_id;
 root_id:=(r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root')::uuid;
 root_sha:=r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root_sha';
 IF result_id IS NULL OR root_id IS NULL OR root_sha IS NULL THEN RETURN NULL;END IF;
 IF p_channel='pixels' THEN
  role:=CASE p_route WHEN 'candidate' THEN 'output_pixels' ELSE 'source_pixels' END;
  locator:=photo.v28_restoration_result_array(p_tenant,p_task,result_id,root_id,
   root_sha,role,p_artifact,p_sha,p_bucket);
  IF locator IS NULL THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha,'bucket',p_bucket,
   'object_key',locator->>'key','object_version',locator->>'version',
   'bytes',(locator->>'bytes')::bigint,'media_type',locator->>'media_type');
 END IF;
 -- Restoration must preserve the original alpha. It is never accepted as a
 -- newly produced output; the selected document names the same source array.
 source_version:=((r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->>'asset_version_id')::uuid;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact
  AND sha256=p_sha AND bucket=p_bucket AND state='ready'
  AND verified_at IS NOT NULL AND object_version NOT IN('','null')
  AND media_type IN('application/x-npy','application/octet-stream')
  AND bytes BETWEEN 14 AND 800065536
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR source_version IS NULL OR
  photo.v2_draft_artifact_scope(p_tenant,p_project,source_version,a.id,a.sha256,
   'WorkingAlpha',r.created_by,r.v28_restoration_session_id,
   ((r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'artifact_id')::uuid,
   (r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'sha256')
   IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,
  'object_key',a.object_key,'object_version',a.object_version,
  'bytes',a.bytes,'media_type',a.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_restoration_selected_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_restoration_selected_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.read_v28_restoration_selected_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
