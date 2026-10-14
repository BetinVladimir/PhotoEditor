-- +goose Up
-- Bounded cross-tenant work discovery, followed by existing tenant-fenced claim.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_next_v30_metadata_barrier()
RETURNS TABLE(tenant_id uuid,child_run_id uuid,expansion_id uuid,lease_token uuid,lease_until timestamptz,parent_task_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;claimed record;previous text:=current_setting('app.tenant_id',true);
BEGIN
 FOR candidate IN SELECT e.tenant_id,e.run_id FROM photo.graph_expansions e
 WHERE e.kind='v30.delivery.metadata/1' AND e.state='open'
 AND (e.next_expansion_at IS NULL OR e.next_expansion_at<=clock_timestamp())
 AND (e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp())
 AND e.v30_metadata_encoded_jcs IS NOT NULL
 AND photo.v30_metadata_barrier_ready(e.tenant_id,e.run_id) IS TRUE
 ORDER BY e.next_expansion_at NULLS FIRST,e.created_at,e.id LIMIT 32 LOOP
  PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
  SELECT * INTO claimed FROM photo.claim_v30_metadata_barrier(candidate.tenant_id,candidate.run_id);
  IF FOUND THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
   RETURN QUERY SELECT candidate.tenant_id,candidate.run_id,claimed.expansion_id,claimed.lease_token,claimed.lease_until,claimed.parent_task_id;
   RETURN;
  END IF;
 END LOOP;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
END $$;
ALTER FUNCTION photo.claim_next_v30_metadata_barrier() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_next_v30_metadata_barrier() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_next_v30_metadata_barrier() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
