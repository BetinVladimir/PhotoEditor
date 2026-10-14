-- +goose Up
-- Classify a Start refusal for a preexisting duplicate reservation without
-- revealing its sibling identity or allowing a new Create.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_cross_key_reconciliation_required(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.v30_archive_uploads d
  JOIN photo.tenants t ON t.id=d.tenant_id
  JOIN photo.projects p ON p.tenant_id=d.tenant_id AND p.id=d.project_id
  WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
   AND d.tenant_id=p_tenant AND d.id=p_upload AND d.actor_id=p_actor AND d.token=p_reservation
   AND d.state='reserved' AND d.expires_at>clock_timestamp() AND t.status='active'
   AND p.archived_at IS NULL AND p.download_allowed
   AND photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session)
   AND EXISTS(
    SELECT 1 FROM photo.v30_archive_uploads prior
    WHERE prior.tenant_id=p_tenant AND prior.id<>d.id AND prior.actor_id=p_actor
     AND prior.project_id=d.project_id
     AND (prior.request_sha=d.request_sha OR (prior.expected_sha=d.expected_sha AND prior.expected_bytes=d.expected_bytes))
     AND (((prior.state='reserved' AND prior.expires_at>clock_timestamp())
         AND (prior.created_at,prior.id)<(d.created_at,d.id))
      OR EXISTS(SELECT 1 FROM photo.v30_archive_writers w
         WHERE w.tenant_id=prior.tenant_id AND w.upload_id=prior.id AND w.kind='begin' AND w.state='open')
      OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
         WHERE g.tenant_id=prior.tenant_id AND g.upload_id=prior.id AND g.state IN('creating','created','aborting')))
   )
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_archive_cross_key_reconciliation_required(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_cross_key_reconciliation_required(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_cross_key_reconciliation_required(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
