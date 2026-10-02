-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_ingest_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; token uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT rr.* INTO r FROM photo.runs rr
 LEFT JOIN photo.ingest_progress p ON p.tenant_id=rr.tenant_id AND p.run_id=rr.id
 WHERE rr.kind='ingest' AND rr.status IN ('queued','running') AND NOT rr.cancel_requested
 AND rr.next_ingest_build_at<=clock_timestamp()
 AND (rr.ingest_build_until IS NULL OR rr.ingest_build_until<=clock_timestamp())
 AND ((p.run_id IS NULL AND NOT rr.graph_sealed) OR p.phase IN ('inspection','decode_build','decode','catalog'))
 ORDER BY rr.next_ingest_build_at,rr.id LIMIT 1 FOR UPDATE OF rr SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.runs SET ingest_build_token=token,ingest_build_until=until_time WHERE id=r.id;
 RETURN QUERY SELECT r.tenant_id,r.id,token,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_ingest_build() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_ingest_build() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_ingest_build() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
