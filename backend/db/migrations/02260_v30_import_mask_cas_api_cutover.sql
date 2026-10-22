-- +goose Up
-- CAS-only API authority. Legacy implementation remains callable by its owner
-- solely through the SECURITY DEFINER CAS wrappers introduced in 02259.
-- +goose StatementBegin
REVOKE ALL ON FUNCTION photo.grant_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid) FROM photo_api;
REVOKE ALL ON FUNCTION photo.revoke_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid) FROM photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
