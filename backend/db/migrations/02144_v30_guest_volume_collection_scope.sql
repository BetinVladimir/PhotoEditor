-- +goose Up
-- A guest volume job must still belong to the collection of the live share
-- session. Enforce the same scope both before consuming the bearer and after
-- the external URL signer returns.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.redeem_download_intent(bytea,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('j.share_id IS DISTINCT FROM i.share_id' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0
  OR position('s.collection_id IS DISTINCT FROM j.collection_id' IN definition)>0 THEN
  RAISE EXCEPTION 'GUEST_VOLUME_REDEEM_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'j.share_id IS DISTINCT FROM i.share_id',
  's.collection_id IS DISTINCT FROM j.collection_id OR j.share_id IS DISTINCT FROM i.share_id');
 EXECUTE definition;

 SELECT pg_get_functiondef('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('j.share_id IS DISTINCT FROM i.share_id' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0
  OR position('s.collection_id IS DISTINCT FROM j.collection_id' IN definition)>0 THEN
  RAISE EXCEPTION 'GUEST_VOLUME_POSTSIGN_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'j.share_id IS DISTINCT FROM i.share_id',
  's.collection_id IS DISTINCT FROM j.collection_id OR j.share_id IS DISTINCT FROM i.share_id');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.redeem_download_intent(bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.redeem_download_intent(bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.redeem_download_intent(bytea,text) TO photo_api,photo_portal;
ALTER FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)
 TO photo_api,photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
