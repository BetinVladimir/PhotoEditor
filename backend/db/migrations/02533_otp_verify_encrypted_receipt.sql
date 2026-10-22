-- +goose Up
-- Global auth metadata; no tenant DELETE allowlist expansion. No plaintext
-- access/refresh/CSRF tokens or caller bootstrap credentials are persisted.
ALTER TABLE photo.auth_challenges
 ADD COLUMN verify_receipt_binding bytea,
 ADD COLUMN verify_receipt_cipher bytea,
 ADD COLUMN verify_receipt_expires_at timestamptz,
 ADD COLUMN verify_receipt_epoch bigint,
 ADD CONSTRAINT auth_verify_receipt_complete CHECK(
  (verify_receipt_binding IS NULL AND verify_receipt_cipher IS NULL
   AND verify_receipt_expires_at IS NULL AND verify_receipt_epoch IS NULL)
  OR (verify_receipt_binding IS NOT NULL AND verify_receipt_cipher IS NOT NULL
   AND verify_receipt_epoch IS NOT NULL AND octet_length(verify_receipt_binding)=32
   AND octet_length(verify_receipt_cipher) BETWEEN 29 AND 32768
   AND verify_receipt_expires_at IS NOT NULL AND verify_receipt_epoch>=0
   AND consumed_at IS NOT NULL));
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
