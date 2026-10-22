-- +goose Up
-- mask.transform may process source-bound regional masks sequentially in the
-- same canonical root. Keep the existing operation/version and SQL upper bound.
UPDATE photo.operation_registry SET timeout_seconds=7200 WHERE operation='mask.transform' AND version='1' AND resource_class='cpu';

-- One primary pair plus at most62 unique regional pairs; one root manifest.
-- Keep per-artifact media/byte bounds and the gateway's128-total-output cap.
UPDATE photo.operation_output_policies SET max_count=63
WHERE operation='mask.transform' AND version='1'
 AND role IN ('transformed_mask','transformed_mask_png');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
