-- +goose Up
-- Global operator trust metadata, not tenant/customer metadata. Provisioning
-- explicitly installs issuer/trust pins; API cannot create or enable issuers.
-- +goose StatementBegin
CREATE TABLE photo.v31_crl_issuers (
 trust_bundle_sha text NOT NULL CHECK(trust_bundle_sha ~ '^[0-9a-f]{64}$'),
 issuer_sha text NOT NULL CHECK(issuer_sha ~ '^[0-9a-f]{64}$'),
 enabled boolean NOT NULL DEFAULT false,
 PRIMARY KEY(trust_bundle_sha,issuer_sha)
);
CREATE TABLE photo.v31_crl_checkpoints (
 trust_bundle_sha text NOT NULL,
 issuer_sha text NOT NULL,
 crl_number numeric(49,0) NOT NULL CHECK(crl_number BETWEEN 0 AND 1461501637330902918203684832716283019655932542975),
 this_update timestamptz NOT NULL,
 crl_sha text NOT NULL CHECK(crl_sha ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(trust_bundle_sha,issuer_sha),
 FOREIGN KEY(trust_bundle_sha,issuer_sha) REFERENCES photo.v31_crl_issuers
);
REVOKE ALL ON photo.v31_crl_issuers,photo.v31_crl_checkpoints FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT ON photo.v31_crl_issuers TO photo_fn_owner;
GRANT SELECT,INSERT,UPDATE ON photo.v31_crl_checkpoints TO photo_fn_owner;
CREATE FUNCTION photo.observe_v31_crl_checkpoint(p_trust text,p_issuer text,p_number text,p_this timestamptz,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE accepted boolean;
BEGIN
 IF coalesce(p_trust,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_issuer,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_number,'') !~ '^(0|[1-9][0-9]{0,48})$'
 OR p_this IS NULL OR NOT isfinite(p_this) OR p_this>clock_timestamp() THEN RETURN false;END IF;
 IF p_number::numeric>1461501637330902918203684832716283019655932542975 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v31_crl_issuers WHERE trust_bundle_sha=p_trust AND issuer_sha=p_issuer AND enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.v31_crl_checkpoints AS old(trust_bundle_sha,issuer_sha,crl_number,this_update,crl_sha)
 VALUES(p_trust,p_issuer,p_number::numeric,p_this,p_sha)
 ON CONFLICT(trust_bundle_sha,issuer_sha) DO UPDATE
 SET crl_number=excluded.crl_number,this_update=excluded.this_update,crl_sha=excluded.crl_sha,observed_at=clock_timestamp()
 WHERE excluded.crl_number>=old.crl_number AND excluded.this_update>=old.this_update
 AND (excluded.crl_number>old.crl_number OR excluded.crl_sha=old.crl_sha)
 RETURNING true INTO accepted;
 RETURN coalesce(accepted,false);
END $$;
ALTER FUNCTION photo.observe_v31_crl_checkpoint(text,text,text,timestamptz,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.observe_v31_crl_checkpoint(text,text,text,timestamptz,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.observe_v31_crl_checkpoint(text,text,text,timestamptz,text) TO photo_api;
CREATE FUNCTION photo.v31_crl_checkpoint_ready(p_trust text,p_issuers text[])
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF coalesce(p_trust,'') !~ '^[0-9a-f]{64}$' OR p_issuers IS NULL
 OR coalesce(array_ndims(p_issuers),0)<>1 OR cardinality(p_issuers) NOT BETWEEN 1 AND 64
 OR EXISTS(SELECT 1 FROM unnest(p_issuers) value WHERE value IS NULL OR value !~ '^[0-9a-f]{64}$')
 OR (SELECT count(DISTINCT value) FROM unnest(p_issuers) value)<>cardinality(p_issuers) THEN RETURN false;END IF;
 RETURN (SELECT count(*) FROM photo.v31_crl_issuers WHERE trust_bundle_sha=p_trust AND enabled AND issuer_sha=ANY(p_issuers))=cardinality(p_issuers);
END $$;
ALTER FUNCTION photo.v31_crl_checkpoint_ready(text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_crl_checkpoint_ready(text,text[]) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_crl_checkpoint_ready(text,text[]) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
