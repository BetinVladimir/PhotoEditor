-- Extend the frozen training snapshot closure to the exact, published
-- sensor-dust evidence object. Keep the existing frozen-recipe/defect checks.
-- +goose Up
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 RENAME TO v23_training_frozen_complete_defect_core;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete_defect_core(uuid,uuid,jsonb)
 FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_complete(
 p_tenant uuid,p_run uuid,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; evidence jsonb; event_id uuid; aid uuid; h bytea;
 sha text;
BEGIN
 IF NOT photo.v23_training_frozen_complete_defect_core(
  p_tenant,p_run,p_manifest) THEN RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF NOT (item->'render_case' ? 'dust_evidence') THEN CONTINUE; END IF;
  IF item->'render_case' ? 'defect_evidence' THEN RETURN false; END IF;
  event_id:=NULLIF(item->>'event_id','')::uuid;
  IF event_id IS NULL THEN RETURN false; END IF;
  evidence:=item->'render_case'->'dust_evidence';
  IF jsonb_typeof(evidence) IS DISTINCT FROM 'object' OR
   (SELECT count(*) FROM jsonb_object_keys(evidence))<>2 OR
   evidence ?& ARRAY['artifact_id','sha256'] IS NOT TRUE THEN
   RETURN false; END IF;
  sha:=evidence->>'sha256';
  IF sha IS NULL OR sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
  h:=public.digest(uuid_send(p_run)||convert_to(
   'v23-dust/'||event_id::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);
  h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF evidence->>'artifact_id' IS DISTINCT FROM aid::text OR
   NOT EXISTS(SELECT 1 FROM photo.backend_documents d
    JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND
     a.id=d.artifact_id AND a.sha256=d.sha256 AND
     a.object_version=d.object_version AND a.state='ready'
    WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND
     d.owner_id=event_id AND d.purpose='v23_dust_holdout' AND
     d.artifact_id=aid AND d.sha256=sha AND d.state='ready' AND
     d.object_version IS NOT NULL AND a.output_role='v23_dust_holdout' AND
     a.bucket=d.bucket AND a.bytes=d.bytes AND
     a.media_type='application/json' AND a.verified_at IS NOT NULL AND
     a.object_key='tenants/'||p_tenant||'/runs/'||p_run||
      '/documents/'||aid AND
     (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
   RETURN false;
  END IF;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
