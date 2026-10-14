-- +goose Up
-- Observe exact current draft revisions for a retained source snapshot.
-- Caller must persist this result before recipe freezing; no object IO here.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_v30_delivery_draft_selection(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE total integer;pins jsonb;row record;canvas text;revision text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT item_count INTO total FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 IF total IS NULL OR total NOT BETWEEN 1 AND 5000 THEN RETURN NULL;END IF;
 pins:='[]'::jsonb;
 FOR row IN SELECT si.*,v.asset_id,s.revision,s.current_document_id FROM photo.snapshot_items si
 JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id AND v.review_state='accepted'
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
 JOIN photo.v2_document_streams s ON s.tenant_id=v.tenant_id AND s.project_id=p_project AND s.asset_version_id=v.id AND s.stream_type='draft'
 JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.project_id=s.project_id AND d.stream_id=s.id AND d.id=s.current_document_id AND d.document_type='EditingRecipeV3'
 JOIN photo.artifacts recipe ON recipe.tenant_id=d.tenant_id AND recipe.id=d.artifact_id AND recipe.sha256=d.sha256
 WHERE si.tenant_id=p_tenant AND si.snapshot_id=p_snapshot AND si.artifact_id=v.master_artifact_id AND si.sha256=v.content_hash
 AND NOT EXISTS(SELECT 1 FROM photo.asset_versions newer WHERE newer.tenant_id=v.tenant_id AND newer.asset_id=v.asset_id AND newer.version_no>v.version_no)
 AND s.approved_document_id IS DISTINCT FROM d.id
 AND recipe.state='ready' AND recipe.verified_at IS NOT NULL AND recipe.output_role='document' AND recipe.media_type='application/json'
 AND recipe.object_version NOT IN('','null') AND (recipe.expires_at IS NULL OR recipe.expires_at>clock_timestamp())
 ORDER BY si.ordinal FOR SHARE OF si,v,a,s,d,recipe LOOP
  canvas:=coalesce(row.payload->>'canvas_hash',row.payload->'binding'->>'canvas_hash');revision:=row.revision::text;
  IF row.ordinal<>jsonb_array_length(pins) OR coalesce(canvas,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
  pins:=pins||jsonb_build_array(jsonb_build_object('ordinal',row.ordinal,'asset_id',row.asset_id,
   'binding',jsonb_build_object('asset_version_id',row.asset_version_id,'source',jsonb_build_object('artifact_id',row.artifact_id,'sha256',row.sha256),'canvas_hash',canvas),
   'request',jsonb_build_object('document_id',row.current_document_id,'expected_revision',revision,'assume_profile',NULL)));
 END LOOP;
 IF jsonb_array_length(pins)<>total THEN RETURN NULL;END IF;
 RETURN pins;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.capture_v30_delivery_draft_selection(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_v30_delivery_draft_selection(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.capture_v30_delivery_draft_selection(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
