-- Every tenant receives the two portable export contracts required by the API.
-- The trigger keeps all tenant creation paths atomic; the backfill covers tenants
-- created before this migration. No model, credential or demo data is seeded.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.seed_default_export_presets()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 INSERT INTO photo.export_presets(tenant_id,name,config,config_hash)
 VALUES
 (NEW.id,'Master',
  '{"format":"tiff","icc_id":"work_rec2020_gamma24_v1","subsampling":"444","alpha":"preserve","metadata_policy":"strip","naming_template":"{original_stem}"}'::jsonb,
  '53ce136942dc0e726e24ce51f6aafcd6fae5edace49b38a8534030720639f826'),
 (NEW.id,'Web',
  '{"format":"jpeg","icc_id":"srgb_v1","size":{"long_edge":2048,"fit":"contain","allow_upscale":false},"quality":90,"subsampling":"444","alpha":"matte","matte_rgb":[1,1,1],"metadata_policy":"strip","naming_template":"{original_stem}_web"}'::jsonb,
  '4d6d9b57d4318d1d0cd33677c5d31ca842c5e5fcd31cfd18c096896817d8b21a');
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seed_default_export_presets() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seed_default_export_presets() FROM PUBLIC;
CREATE TRIGGER tenant_default_export_presets AFTER INSERT ON photo.tenants
 FOR EACH ROW EXECUTE FUNCTION photo.seed_default_export_presets();

INSERT INTO photo.export_presets(tenant_id,name,config,config_hash)
SELECT t.id,p.name,p.config,p.config_hash
FROM photo.tenants t
CROSS JOIN (VALUES
 ('Master','{"format":"tiff","icc_id":"work_rec2020_gamma24_v1","subsampling":"444","alpha":"preserve","metadata_policy":"strip","naming_template":"{original_stem}"}'::jsonb,'53ce136942dc0e726e24ce51f6aafcd6fae5edace49b38a8534030720639f826'),
 ('Web','{"format":"jpeg","icc_id":"srgb_v1","size":{"long_edge":2048,"fit":"contain","allow_upscale":false},"quality":90,"subsampling":"444","alpha":"matte","matte_rgb":[1,1,1],"metadata_policy":"strip","naming_template":"{original_stem}_web"}'::jsonb,'4d6d9b57d4318d1d0cd33677c5d31ca842c5e5fcd31cfd18c096896817d8b21a')
) AS p(name,config,config_hash)
WHERE NOT EXISTS(SELECT 1 FROM photo.export_presets e WHERE e.tenant_id=t.id AND e.name=p.name AND e.archived_at IS NULL);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
