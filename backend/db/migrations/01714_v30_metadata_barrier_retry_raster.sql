-- +goose Up
-- Existing barrier replay retains physical raster evidence.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)'::regprocedure);
 needle:='AND existing.state=''open'' AND existing.expected_count=1 AND NOT r.graph_sealed';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'METADATA_BARRIER_RETRY_DEFINITION_DRIFT';
 END IF;
 replacement:=needle||' AND EXISTS(SELECT 1 FROM photo.artifacts raster
 WHERE raster.tenant_id=p_tenant AND raster.id=((p_encoded::jsonb)->''artifact''->>''artifact_id'')::uuid
 AND raster.sha256=(p_encoded::jsonb)->''artifact''->>''sha256'' AND raster.bytes=p_bytes
 AND raster.state=''ready'' AND raster.verified_at IS NOT NULL AND raster.output_role=''encoded_image''
 AND raster.object_version IS NOT NULL AND raster.object_version NOT IN('''',''null'')
 AND (raster.expires_at IS NULL OR raster.expires_at>clock_timestamp()))';
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
