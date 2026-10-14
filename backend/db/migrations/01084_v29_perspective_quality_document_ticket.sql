-- +goose Up
-- Ticket lookup stays inside the pin transaction; scheduler receives no table SELECT grant.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_perspective_quality_document(p_tenant uuid,p_run uuid,p_owner uuid,p_variant text,p_sha text,p_size bigint,p_bucket text,p_root text,p_image text,p_params text)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected uuid;h bytea;pinned_id uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_size IS DISTINCT FROM octet_length(p_params)::bigint
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex')
 OR p_variant IS NULL OR p_variant<>'master' THEN RETURN;END IF;
 h:=public.digest(uuid_send(p_run)||convert_to('quality.check:master','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_owner IS DISTINCT FROM expected OR expected IS NULL THEN RETURN;END IF;
 pinned_id:=photo.pin_v29_perspective_quality_document(p_tenant,p_run,p_variant,p_root,p_image,p_params,p_bucket);
 IF pinned_id IS NULL THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.artifact_id=pinned_id AND b.purpose='task_params';
 IF NOT FOUND OR d.state NOT IN('pending','ready') THEN RETURN;END IF;
 IF d.state='ready' AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
  AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
  AND a.output_role='task_params' AND a.media_type='application/json' AND a.verified_at IS NOT NULL
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN;END IF;
 RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v29_perspective_quality_document(uuid,uuid,uuid,text,text,bigint,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_perspective_quality_document(uuid,uuid,uuid,text,text,bigint,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_perspective_quality_document(uuid,uuid,uuid,text,text,bigint,text,text,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
