-- +goose Up
-- Operation-computed coverage is reachable from its typed report. This does not
-- modify image editing algorithms or claim that selected pixels changed value.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
SELECT op,'1','generated_pixels','["application/octet-stream"]'::jsonb,800008192,1
FROM unnest(ARRAY['image.generate','image.inpaint','image.outpaint']) AS op;
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
SELECT op,'1','generated_mask','["application/json"]'::jsonb,2097152,1
FROM unnest(ARRAY['image.generate','image.inpaint','image.outpaint']) AS op;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
