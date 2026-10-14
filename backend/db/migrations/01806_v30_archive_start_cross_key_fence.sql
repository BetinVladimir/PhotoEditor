-- +goose Up
-- Reserve's cross-key check does not cover duplicate reservations committed
-- before 01805. Serialize both Start implementations on the same tenant row
-- Reserve locks, then refuse any unresolved sibling or an older live sibling.
-- +goose StatementBegin
DO $$
DECLARE definition text;signature regprocedure;tenant_lock text;
 guard_template text:=' -- v30_archive_start_cross_key_fence
 IF EXISTS(
  SELECT 1 FROM photo.v30_archive_uploads prior
  WHERE prior.tenant_id=p_tenant AND prior.id<>p_upload
   AND prior.actor_id=p_actor AND prior.project_id=d.project_id
   AND (prior.request_sha=d.request_sha OR (prior.expected_sha=d.expected_sha AND prior.expected_bytes=d.expected_bytes))
   AND (((prior.state=''reserved'' AND prior.expires_at>clock_timestamp())
       AND (prior.created_at,prior.id)<(d.created_at,d.id))
    OR EXISTS(SELECT 1 FROM photo.v30_archive_writers w
       WHERE w.tenant_id=prior.tenant_id AND w.upload_id=prior.id AND w.kind=''begin'' AND w.state=''open'')
    OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
       WHERE g.tenant_id=prior.tenant_id AND g.upload_id=prior.id AND g.state IN(''creating'',''created'',''aborting'')))
 ) THEN __RETURN__;END IF;
 ';
 needle text;guard text;
BEGIN
 tenant_lock:=' PERFORM id FROM photo.tenants WHERE id=p_tenant AND status=''active'' FOR SHARE;IF NOT FOUND THEN ';
 FOR signature IN SELECT unnest(ARRAY[
  'photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)'::regprocedure,
  'photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)'::regprocedure]) LOOP
  SELECT pg_get_functiondef(signature) INTO definition;
  IF signature::text LIKE '%start_v30_archive_begin_generation%'
  THEN
   needle:=' IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.state=''open'')';
   guard:=replace(guard_template,'__RETURN__','RETURN');
  ELSE
   needle:=' IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND (state=''open'' OR kind=''begin''))';
   guard:=replace(guard_template,'__RETURN__','RETURN NULL');
  END IF;
  IF definition IS NULL OR position('v30_archive_start_cross_key_fence' IN definition)>0
   OR position(tenant_lock IN definition)=0 OR position(needle IN definition)=0
  THEN RAISE EXCEPTION 'ARCHIVE_START_CROSS_KEY_SHAPE: %',signature;END IF;
  definition:=replace(definition,tenant_lock,
   replace(tenant_lock,'FOR SHARE','FOR UPDATE'));
  definition:=replace(definition,needle,guard||needle);
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
