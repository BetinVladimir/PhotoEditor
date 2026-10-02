-- +goose Up
ALTER TABLE photo.runs ADD COLUMN ingest_build_token uuid;
ALTER TABLE photo.runs ADD COLUMN ingest_build_until timestamptz;
ALTER TABLE photo.runs ADD COLUMN next_ingest_build_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE photo.runs ADD CONSTRAINT ingest_build_lease_pair CHECK((ingest_build_token IS NULL)=(ingest_build_until IS NULL));
CREATE INDEX ingest_build_pending ON photo.runs(next_ingest_build_at,id) WHERE kind='ingest' AND status IN ('queued','running') AND NOT cancel_requested;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_ingest_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; token uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT rr.* INTO r FROM photo.runs rr
 LEFT JOIN photo.ingest_progress p ON p.tenant_id=rr.tenant_id AND p.run_id=rr.id
 WHERE rr.kind='ingest' AND rr.status IN ('queued','running') AND NOT rr.cancel_requested
 AND rr.next_ingest_build_at<=clock_timestamp()
 AND (rr.ingest_build_until IS NULL OR rr.ingest_build_until<=clock_timestamp())
 AND ((p.run_id IS NULL AND NOT rr.graph_sealed) OR p.phase IN ('inspection','decode_build'))
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
-- +goose StatementBegin
CREATE FUNCTION photo.renew_ingest_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET ingest_build_until=clock_timestamp()+interval '60 seconds'
 WHERE id=p_run AND kind='ingest' AND status IN ('queued','running') AND NOT cancel_requested
 AND ingest_build_token=p_token AND ingest_build_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_ingest_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_ingest_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_ingest_build(uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.release_ingest_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET ingest_build_token=NULL,ingest_build_until=NULL,next_ingest_build_at=clock_timestamp()+interval '5 seconds'
 WHERE id=p_run AND kind='ingest' AND ingest_build_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_ingest_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_ingest_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_ingest_build(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
