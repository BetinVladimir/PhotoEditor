-- +goose Up
-- Materialize the exact human-selected relight image, without rerendering.
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v28_relight_version(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid,
 p_decision_hash text,p_base text,p_image text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 selected record;body jsonb;base jsonb;image jsonb;identity uuid;next_no bigint;target_project uuid;
BEGIN
 IF p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$'
 OR p_base IS NULL OR octet_length(p_base) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind='image.relight' AND state='decided' AND revision=2;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=v.relight_params_jcs::jsonb;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant
  AND id=(body->'base'->>'asset_version_id')::uuid AND review_state='accepted';
 IF NOT FOUND OR src.master_artifact_id IS DISTINCT FROM v.base_artifact_id THEN RETURN NULL;END IF;
 SELECT w.project_id INTO target_project FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=v.run_id;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=target_project
  AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Serialize version numbering before the authority reader acquires asset locks.
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id=src.asset_id
  AND a.project_id=target_project AND a.deleted_at IS NULL AND a.upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO selected FROM photo.get_v28_relight_review_selection(p_tenant,p_actor,p_session,p_review);
 IF NOT FOUND OR selected.decision_hash IS DISTINCT FROM p_decision_hash THEN RETURN NULL;END IF;
 base:=p_base::jsonb;image:=p_image::jsonb;
 IF jsonb_typeof(base) IS DISTINCT FROM 'object' OR jsonb_typeof(image) IS DISTINCT FROM 'object'
 OR encode(sha256(convert_to(p_base,'UTF8')),'hex') IS DISTINCT FROM src.content_hash
 OR base->>'dtype' IS DISTINCT FROM 'float32' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR (base-'pixels') IS DISTINCT FROM (image-'pixels') THEN RETURN NULL;END IF;
 IF selected.route='skip' THEN RETURN NULL;END IF;
 IF selected.route='base' THEN
  IF selected.image_id=src.master_artifact_id AND selected.image_sha=src.content_hash
   AND p_image=p_base THEN RETURN src.id;END IF;
  RETURN NULL;
 END IF;
 IF selected.route IS DISTINCT FROM 'candidate'
 OR selected.image_id IS DISTINCT FROM v.candidate_artifact_id
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM selected.image_sha
 OR image->'pixels'->>'artifact_id' IS NULL
 OR image->'pixels'->>'artifact_id' IS NOT DISTINCT FROM base->'pixels'->>'artifact_id'
 THEN RETURN NULL;END IF;
 -- Child references must belong to this exact adopted result, not merely be
 -- ready artifacts elsewhere in the tenant. These links are also GC roots.
 IF EXISTS(SELECT 1 FROM (VALUES
  (image->'pixels','$/working_image/pixels'),
  (image->'alpha','$/working_image/alpha')) required(ref,path)
  WHERE required.ref IS NOT NULL AND required.ref<>'null'::jsonb AND NOT EXISTS(
   SELECT 1 FROM photo.v2_artifact_links link JOIN photo.v2_workflows workflow
    ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
   WHERE link.tenant_id=p_tenant AND workflow.run_id=v.run_id AND link.project_id=target_project
    AND link.artifact_id=(required.ref->>'artifact_id')::uuid
    AND link.sha256=required.ref->>'sha256'
    AND link.purpose=encode(sha256(convert_to('tasks/'||v.task_id::text||'/'||required.path,'UTF8')),'hex')))
 THEN RETURN NULL;END IF;
 -- All retained image children must still be ready exact-version objects.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
  (image->'pixels'->>'artifact_id')::uuid,(image->'alpha'->>'artifact_id')::uuid) ORDER BY a.id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(image->'pixels'->>'artifact_id')::uuid AND a.sha256=image->'pixels'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR (image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb AND NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(image->'alpha'->>'artifact_id')::uuid AND a.sha256=image->'alpha'->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN NULL;END IF;
 identity:=photo.semantic_uuid_v5(p_review,'V28.RelightAppliedVersion');
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=src.id
   AND old.master_artifact_id=selected.image_id AND old.content_hash=selected.image_sha
   AND old.metadata=image AND old.created_by_run_id=v.run_id AND old.review_state='accepted'
   AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity
    AND source_version_id=src.id AND role='relight_source' AND ordinal=0) THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF photo.v28_relight_review_authority(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,
  created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,selected.image_id,src.id,v.run_id,selected.image_sha,image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 VALUES(p_tenant,identity,src.id,'relight_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
