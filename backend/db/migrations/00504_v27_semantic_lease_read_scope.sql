-- +goose Up
-- Lease reads stay behind SECURITY DEFINER; no scheduler table grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_lease_live(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.tenants t ON t.id=r.tenant_id
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
 AND e.kind='v2.semantic.assemble/1' AND e.state='open' AND e.expansion_token=p_token
 AND e.expansion_lease_until>clock_timestamp() AND r.kind='v2' AND r.status='running'
 AND r.graph_sealed AND NOT r.cancel_requested AND t.status='active');
$$;
ALTER FUNCTION photo.v27_semantic_assembly_lease_live(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_lease_live(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_lease_live(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
