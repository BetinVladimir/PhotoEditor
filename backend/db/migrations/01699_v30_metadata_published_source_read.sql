-- +goose Up
-- Scheduler reads normalized JSON evidence only, never original object location.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_metadata_source(p_tenant uuid,p_parent uuid,p_binding jsonb,p_metadata jsonb,p_policy text,p_approved text[])
RETURNS TABLE(publication_jcs text,publication_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;envelope jsonb;proof jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_policy IS NULL OR p_policy NOT IN('copyright','approved_exif') THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent
 AND purpose='v30_metadata_source' AND state='ready' AND artifact_id=(p_metadata->>'artifact_id')::uuid;
 IF NOT FOUND OR d.v30_metadata_source_jcs IS NULL THEN RETURN;END IF;
 envelope:=d.v30_metadata_source_jcs::jsonb;
 IF envelope->'metadata_source' IS DISTINCT FROM p_metadata OR envelope->'scope'->'binding' IS DISTINCT FROM p_binding
 OR envelope->>'policy' IS DISTINCT FROM p_policy THEN RETURN;END IF;
 proof:=jsonb_build_object('metadata_source',p_metadata,'metadata_jcs',envelope->>'metadata_jcs','binding',p_binding,
 'policy',p_policy,'frozen_jcs',envelope->>'frozen_jcs','frozen_sha256',envelope->>'frozen_sha256');
 IF photo.v30_metadata_published_source_ready(p_tenant,p_parent,proof,p_approved) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT d.v30_metadata_source_jcs,d.v30_metadata_source_sha;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_metadata_source(uuid,uuid,jsonb,jsonb,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_metadata_source(uuid,uuid,jsonb,jsonb,text,text[]) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_metadata_source(uuid,uuid,jsonb,jsonb,text,text[]) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
