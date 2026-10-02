-- Expose graph identity on apply leases without changing the legacy function
-- signature used by already deployed schedulers.
-- +goose Up
ALTER TABLE photo.apply_builds ADD COLUMN stage_index int CHECK(stage_index BETWEEN 0 AND 63);
ALTER TABLE photo.apply_builds ADD CONSTRAINT apply_builds_stage_version_check CHECK((graph_version=3 AND stage_index IS NULL) OR graph_version=4);

-- +goose StatementBegin
CREATE FUNCTION photo.claim_apply_build_versioned()
RETURNS TABLE(tenant_id uuid,run_id uuid,source_collection_id uuid,style_version_id uuid,phase text,item_count int,lease_token uuid,lease_until timestamptz,graph_version int,stage_index int)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,b.graph_version,b.stage_index
 FROM photo.claim_apply_build() c
 JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND b.lease_token=c.lease_token
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_apply_build_versioned() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_apply_build_versioned() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_apply_build_versioned() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
