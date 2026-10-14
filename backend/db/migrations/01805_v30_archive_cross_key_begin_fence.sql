-- +goose Up
-- A different idempotency key changes the upload UUID. It must not evade an
-- unresolved Begin for the same actor and frozen archive request. Reserve
-- already locks the tenant quota row before this insertion point, serializing
-- concurrent keys for one tenant.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:=' SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;';
BEGIN
 SELECT pg_get_functiondef('photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0 OR position('SELECT * INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.actor_id=p_actor AND x.key=p_key FOR UPDATE;' IN definition)=0
 OR position('v30_archive_cross_key_begin_fence' IN definition)>0 THEN RAISE EXCEPTION 'ARCHIVE_CROSS_KEY_RESERVE_SHAPE';END IF;
 definition:=replace(definition,needle,
 ' -- v30_archive_cross_key_begin_fence
 IF EXISTS(
   SELECT 1 FROM photo.v30_archive_uploads prior
   WHERE prior.tenant_id=p_tenant AND prior.actor_id=p_actor AND prior.project_id=pid
    AND (prior.request_sha=request_sha OR (prior.expected_sha=sha AND prior.expected_bytes=size)) AND prior.key<>p_key
    AND ((prior.state=''reserved'' AND prior.expires_at>clock_timestamp())
     OR EXISTS(SELECT 1 FROM photo.v30_archive_writers w
       WHERE w.tenant_id=prior.tenant_id AND w.upload_id=prior.id AND w.kind=''begin'' AND w.state=''open'')
     OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
       WHERE g.tenant_id=prior.tenant_id AND g.upload_id=prior.id AND g.state IN(''creating'',''created'',''aborting'')))
  ) THEN RETURN QUERY SELECT ''reconcile'',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 '||needle);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
