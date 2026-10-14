-- +goose Up
-- Private current-locator comparison only. Not a graph seal or byte verifier.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_witnesses_current(p_tenant uuid,p_run uuid,p_root text,p_image text,p_witnesses jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s record;loc record;item jsonb;ref jsonb;root jsonb;image jsonb;
 frames integer;fields integer;expected integer;role text;path text;bucket text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_root IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_witnesses) IS DISTINCT FROM 'array'
 OR octet_length(p_witnesses::text)>1048576 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v29_multi_preview_intent_source(p_tenant,p_run);
 IF NOT FOUND OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM s.root_sha THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=s.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v29_multi_preview_task_id FOR UPDATE;
 -- Stable artifact lock order before resolving any locator.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id IN (SELECT (value->'ref'->>'artifact_id')::uuid FROM jsonb_array_elements(p_witnesses)) ORDER BY a.id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_multi_preview_intent_source(p_tenant,p_run) fresh
 WHERE fresh.result_id=s.result_id AND fresh.root_id=s.root_id AND fresh.root_sha=s.root_sha
 AND fresh.attempt=s.attempt AND fresh.fence=s.fence) THEN RETURN false;END IF;
 root:=p_root::jsonb;image:=p_image::jsonb;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM root->'working_image'->>'sha256'
 OR image->>'dtype' IS DISTINCT FROM 'float32' OR image->'alpha' IS NULL OR image->'alpha'='null'::jsonb THEN RETURN false;END IF;
 frames:=jsonb_array_length(s.params_jcs::jsonb->'frames');
 fields:=CASE WHEN s.params_jcs::jsonb->>'projection'='cylindrical' THEN frames ELSE 0 END;
 expected:=7+frames+fields;
 IF frames NOT BETWEEN 2 AND 30 OR jsonb_array_length(p_witnesses)<>expected
 OR (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(p_witnesses))<>expected THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_witnesses) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object'
  OR NOT item ?& ARRAY['ref','path','contract','object_key','object_version','sha256','bytes','media_type']
  OR (item-'ref'-'path'-'contract'-'object_key'-'object_version'-'sha256'-'bytes'-'media_type')<>'{}'::jsonb THEN RETURN false;END IF;
  ref:=item->'ref';role:=item->>'contract';path:=item->>'path';
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT ref ?& ARRAY['artifact_id','sha256']
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR item->>'sha256' IS DISTINCT FROM ref->>'sha256' THEN RETURN false;END IF;
  IF path='$' AND role='ImageResult' THEN
   IF ref IS DISTINCT FROM jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha) THEN RETURN false;END IF;
   SELECT * INTO loc FROM photo.v29_adopted_multi_object(p_tenant,s.project_id,r.v29_multi_preview_task_id,s.actor_id,s.auth_session_id,r.config_snapshot->>'operation',s.root_id,s.root_sha);
  ELSE
   SELECT * INTO loc FROM photo.v29_adopted_multi_child_object(p_tenant,s.project_id,r.v29_multi_preview_task_id,s.actor_id,s.auth_session_id,r.config_snapshot->>'operation',s.root_id,s.root_sha,(ref->>'artifact_id')::uuid,ref->>'sha256',path,role);
  END IF;
  IF NOT FOUND OR item->>'object_key' IS DISTINCT FROM loc.object_key
  OR item->>'object_version' IS DISTINCT FROM loc.object_version
  OR item->'bytes' IS DISTINCT FROM to_jsonb(loc.bytes)
  OR item->>'media_type' IS DISTINCT FROM loc.media_type THEN RETURN false;END IF;
  IF bucket IS NULL THEN bucket:=loc.bucket;ELSIF bucket IS DISTINCT FROM loc.bucket THEN RETURN false;END IF;
 END LOOP;
 -- Every required singleton must match the hashed root/image documents.
 FOR ref,role,path IN SELECT * FROM (VALUES
  (jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha),'ImageResult'::text,'$'::text),
  (root->'working_image','WorkingImage','$/working_image'),(root->'changed_mask','MaskRef','$/changed_mask'),
  (root->'report','ImageReport','$/report'),(image->'pixels','WorkingPixels','$/working_image/pixels'),
  (image->'alpha','WorkingAlpha','$/working_image/alpha')) AS required(ref,role,path) LOOP
  IF (SELECT count(*) FROM jsonb_array_elements(p_witnesses) w WHERE w->'ref'=ref AND w->>'contract'=role AND w->>'path'=path)<>1 THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(*) FROM jsonb_array_elements(p_witnesses) w WHERE w->>'contract'='MaskPixels' AND w->>'path'='$/changed_mask/artifact')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_witnesses) w WHERE w->>'contract'='Artifact' AND w->>'path'='$/report/registrations/*')<>frames
 OR (SELECT count(*) FROM jsonb_array_elements(p_witnesses) w WHERE w->>'contract'='DenseWarpField' AND w->>'path'='$/report/registrations/*/inverse_field')<>fields THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_multi_preview_witnesses_current(uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_witnesses_current(uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
