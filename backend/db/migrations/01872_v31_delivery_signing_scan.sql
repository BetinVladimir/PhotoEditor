-- +goose Up
-- Independent round-robin cursor for bounded required-provenance discovery.
-- The export admission cursor remains unchanged. Advancing this cursor never
-- credits a signer, a signed result or package assembly.
ALTER TABLE photo.graph_expansions ADD COLUMN v31_signing_scan_cursor integer
 NOT NULL DEFAULT 0 CHECK (v31_signing_scan_cursor BETWEEN 0 AND 80000);

-- +goose StatementBegin
CREATE FUNCTION photo.next_v31_delivery_signing_page(
 p_expansion uuid,p_token uuid,p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;parent uuid;x photo.graph_expansions%ROWTYPE;
 r photo.runs%ROWTYPE;frozen jsonb;start_at integer;end_at integer;
BEGIN
 IF p_expansion IS NULL OR p_token IS NULL OR p_limit IS NULL
  OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN NULL;END IF;
 SELECT tenant_id,run_id INTO tenant,parent FROM photo.graph_expansions
  WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=tenant AND id=parent
  AND kind='v2' AND status='running' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion
  AND tenant_id=tenant AND run_id=parent AND kind='v2.delivery/1'
  AND state='open' AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR x.expected_count NOT BETWEEN 1 AND 80000
  OR x.cursor IS NULL OR x.cursor !~ '^[0-9]+$'
  OR x.v31_signing_scan_cursor>=x.expected_count THEN RETURN NULL;END IF;
 IF x.cursor::bigint<>x.expected_count THEN RETURN NULL;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=tenant AND b.run_id=parent
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params'
  AND b.state='ready' AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_id' IS NULL
  OR frozen->'policy'->>'signing_policy_sha' !~ '^[0-9a-f]{64}$'
  THEN RETURN NULL;END IF;
 start_at:=x.v31_signing_scan_cursor;
 end_at:=least(start_at+p_limit,x.expected_count::integer);
 UPDATE photo.graph_expansions SET v31_signing_scan_cursor=
  CASE WHEN end_at=x.expected_count THEN 0 ELSE end_at END
 WHERE id=x.id AND tenant_id=tenant AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN start_at;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v31_delivery_signing_page(uuid,uuid,integer)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v31_delivery_signing_page(uuid,uuid,integer)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.next_v31_delivery_signing_page(uuid,uuid,integer)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
