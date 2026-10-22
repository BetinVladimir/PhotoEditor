-- +goose Up
-- Promote only the completed, retained shown-copy result; never rerender pixels.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_semantic_shown_version(p_tenant uuid,p_target uuid,p_actor uuid,p_session uuid,p_image text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE target photo.semantic_targets%ROWTYPE;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 proof record;image jsonb;identity uuid;next_no bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 SELECT * INTO target FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=p_target;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=target.project_id AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=target.asset_version_id AND review_state='accepted';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=src.asset_id AND project_id=target.project_id
 AND deleted_at IS NULL AND upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=target.child_run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=target.child_run_id ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=p_target FOR UPDATE;
 -- Re-read authenticated canonical/adoption closure after lifecycle locks.
 SELECT * INTO proof FROM photo.semantic_shown_applied_image(p_tenant,p_target,p_actor,p_session);
 IF NOT FOUND THEN RETURN NULL;END IF;
 image:=p_image::jsonb;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM proof.sha256
 OR jsonb_typeof(image) IS DISTINCT FROM 'object' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR proof.artifact_id=src.master_artifact_id THEN RETURN NULL;END IF;
 identity:=photo.semantic_uuid_v5(proof.task_id,'V27.ShownAppliedVersion');
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=src.id AND old.master_artifact_id=proof.artifact_id
  AND old.content_hash=proof.sha256 AND old.metadata=image AND old.created_by_run_id=proof.child_run_id AND old.review_state='accepted'
  AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity
   AND source_version_id=src.id AND role='semantic_source' AND ordinal=0) THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,proof.artifact_id,src.id,proof.child_run_id,proof.sha256,image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 VALUES(p_tenant,identity,src.id,'semantic_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.materialize_semantic_shown_version(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_semantic_shown_version(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.materialize_semantic_shown_version(uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
