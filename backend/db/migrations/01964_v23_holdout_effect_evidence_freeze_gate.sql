-- A holdout effect reference is not executable until its review-owned
-- publication, SQL registration and worker closure are installed. Keep the
-- existing frozen-recipe proof and reject a caller-injected evidence ref.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v23_training_frozen_complete(p_tenant uuid,p_run uuid,
 p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; side text; expected jsonb; aid uuid; h bytea;
 event_id uuid; sha text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR jsonb_typeof(p_manifest)<>'object'
  OR jsonb_typeof(p_manifest->'examples')<>'array' OR
  jsonb_array_length(p_manifest->'examples') NOT BETWEEN 40 AND 5000 THEN
  RETURN false;
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF jsonb_typeof(item->'render_case') IS DISTINCT FROM 'object' OR
   (item->'render_case') ? 'defect_evidence' THEN RETURN false; END IF;
  event_id:=NULLIF(item->>'event_id','')::uuid;
  IF event_id IS NULL THEN RETURN false; END IF;
  FOREACH side IN ARRAY ARRAY['base','approved'] LOOP
   expected:=item->'render_case'->(CASE WHEN side='base' THEN
    'base_recipe' ELSE 'approved_recipe' END);
   sha:=expected->>'sha256';
   IF sha IS NULL OR sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
   h:=public.digest(uuid_send(p_run)||convert_to(
    'v23-frozen/'||side||'/'||event_id::text,'UTF8'),'sha1');
   h:=set_byte(h,6,(get_byte(h,6)&15)|80);
   h:=set_byte(h,8,(get_byte(h,8)&63)|128);
   aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
   IF expected->>'artifact_id' IS DISTINCT FROM aid::text OR
    NOT EXISTS(SELECT 1 FROM photo.backend_documents d
     JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND
      a.id=d.artifact_id AND a.sha256=d.sha256 AND
      a.object_version=d.object_version AND a.state='ready'
     WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND
      d.owner_id=event_id AND d.purpose='v23_frozen_'||side AND
      d.artifact_id=aid AND d.sha256=sha AND d.state='ready' AND
      d.object_version IS NOT NULL AND a.output_role='v23_frozen_recipe')
    THEN RETURN false;
   END IF;
  END LOOP;
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
