-- +goose Up
-- Adds a reference-asset geometry version with every ordered source retained; never rewrites the original or its edits.
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v29_multi_version(p_tenant uuid,p_project uuid,p_operation text,p_task uuid,
 p_actor uuid,p_session uuid,p_decision jsonb,p_image text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE q record;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 target jsonb;image jsonb;identity uuid;h bytea;next_no bigint; params jsonb;frame jsonb;p_version uuid;n integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 -- Save repeats caller authority and pins the exact decision under lifecycle locks.
 IF NOT photo.save_v29_multi_apply_decision(p_tenant,p_project,p_operation,p_task,p_actor,p_session,p_decision) THEN RETURN NULL;END IF;
 SELECT * INTO q FROM photo.v29_multi_quality_review(p_tenant,p_project,p_operation,p_task,p_actor,p_session,
 (p_decision->>'result_id')::uuid,p_decision->'root'->>'sha256');
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT (r.config_snapshot->>'multi_params_jcs')::jsonb INTO params FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=q.quality_run_id;
 n:=jsonb_array_length(params->'frames');
 IF n IS NULL OR n NOT BETWEEN 2 AND 30 THEN RETURN NULL;END IF;
 p_version:=(params->'frames'->((params->>'reference_ordinal')::integer)->>'asset_version_id')::uuid;
 target:=(q.report::jsonb)->'target';image:=p_image::jsonb;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM target->>'sha256'
 OR image->>'dtype' IS DISTINCT FROM 'float32' THEN RETURN NULL;END IF;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=p_version AND review_state='accepted';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=src.asset_id AND project_id=p_project
 AND deleted_at IS NULL AND upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Identity bypass would not be a new geometry version.
 IF (target->>'artifact_id')::uuid=src.master_artifact_id THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(target->>'artifact_id')::uuid FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v29_adopted_multi_child_object(p_tenant,p_project,p_task,p_actor,p_session,p_operation,
 (p_decision->'root'->>'artifact_id')::uuid,p_decision->'root'->>'sha256',
 (target->>'artifact_id')::uuid,target->>'sha256','$/working_image','WorkingImage')) THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(q.quality_run_id)||convert_to('V29.MultiAppliedVersion','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=p_version AND old.master_artifact_id=(target->>'artifact_id')::uuid
   AND old.content_hash=target->>'sha256' AND old.metadata=image AND old.created_by_run_id=q.quality_run_id
   AND (SELECT count(*) FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity)=n
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(params->'frames') WITH ORDINALITY f(frame,ord)
    WHERE NOT EXISTS(SELECT 1 FROM photo.asset_version_sources x WHERE x.tenant_id=p_tenant AND x.version_id=identity
     AND x.source_version_id=(f.frame->>'asset_version_id')::uuid AND x.role=p_operation AND x.ordinal=f.ord-1))
  THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,(target->>'artifact_id')::uuid,p_version,q.quality_run_id,target->>'sha256',image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 SELECT p_tenant,identity,(f.frame->>'asset_version_id')::uuid,p_operation,(f.ord-1)::integer
 FROM jsonb_array_elements(params->'frames') WITH ORDINALITY f(frame,ord);
 -- Existing asset-version and V2 closure reference rules retain master/children.
 -- No source draft, collection selection or source-coordinate mask is copied.
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
