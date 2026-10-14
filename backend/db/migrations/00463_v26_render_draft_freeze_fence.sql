-- +goose Up
ALTER TABLE photo.v2_draft_uploads
 ADD COLUMN render_draft_document_id uuid,
 ADD COLUMN render_draft_revision bigint,
 ADD CONSTRAINT v26_render_draft_freeze_pair CHECK (
  (render_draft_document_id IS NULL AND render_draft_revision IS NULL)
  OR (render_draft_document_id IS NOT NULL AND render_draft_revision IS NOT NULL AND render_draft_revision>0)),
 ADD CONSTRAINT v26_render_draft_freeze_document FOREIGN KEY(tenant_id,render_draft_document_id)
  REFERENCES photo.v2_documents(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
-- +goose StatementBegin
CREATE FUNCTION photo.register_v26_render_draft_recipe(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text,
 p_draft_document uuid,p_expected bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE stream photo.v2_document_streams%ROWTYPE; upload photo.v2_draft_uploads%ROWTYPE;
 draft photo.v2_documents%ROWTYPE; body jsonb;
BEGIN
 IF p_tenant IS NULL OR p_version IS NULL OR p_document IS NULL OR p_draft_document IS NULL
  OR p_document=p_draft_document OR p_actor IS NULL OR p_session IS NULL
  OR p_artifact IS NULL OR p_source IS NULL OR p_expected IS NULL OR p_expected<1
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
  OR encode(public.digest(convert_to(p_raw,'UTF8'),'sha256'),'hex')<>p_sha THEN RETURN false;END IF;
 BEGIN body:=p_raw::jsonb; EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 -- Use the shared mutation lock order. No object IO runs under these locks.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT s.* INTO stream FROM photo.v2_document_streams s
  JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.stream_id=s.id AND d.project_id=s.project_id
  WHERE s.tenant_id=p_tenant AND s.asset_version_id=p_version AND s.stream_type='draft'
   AND d.id=p_draft_document AND s.current_document_id=d.id FOR UPDATE OF s;
 IF NOT FOUND OR stream.revision<>p_expected
  OR NOT photo.v2_editor_authorized(p_tenant,stream.project_id,p_actor,p_session) THEN RETURN false;END IF;
 SELECT * INTO draft FROM photo.v2_documents
  WHERE tenant_id=p_tenant AND id=p_draft_document AND project_id=stream.project_id
   AND stream_id=stream.id AND document_type='EditingRecipeV3';
 IF NOT FOUND OR draft.sha256 IS DISTINCT FROM body->>'recipe_sha256'
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=draft.artifact_id
   AND a.sha256=draft.sha256 AND a.state='ready' AND a.output_role='document'
   AND a.media_type='application/json' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO upload FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document FOR UPDATE;
 IF NOT FOUND OR upload.project_id<>stream.project_id OR upload.asset_version_id<>p_version
  OR (upload.render_authored_sha IS NOT NULL AND upload.render_authored_sha<>draft.sha256)
  OR (upload.render_draft_document_id IS NOT NULL AND
   (upload.render_draft_document_id<>p_draft_document OR upload.render_draft_revision<>p_expected)) THEN RETURN false;END IF;
 IF NOT photo.register_v26_render_recipe(p_tenant,p_version,p_document,p_actor,p_session,
  p_artifact,p_sha,p_source,p_source_sha,p_raw) THEN RETURN false;END IF;
 UPDATE photo.v2_draft_uploads SET render_draft_document_id=p_draft_document,render_draft_revision=p_expected
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
