-- +goose Up
-- Completed delivery observations must retain current watermark authority,
-- including metadata outputs and observations after parent expansion closure.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_watermark_pinned_source_ready(uuid,uuid,uuid,uuid,uuid,text,text)'::regprocedure);
 needle:='r.status NOT IN(''draft'',''running'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_RETAINED_SOURCE_DEFINITION_DRIFT';END IF;
 definition:=replace(definition,'photo.v30_watermark_pinned_source_ready(','photo.v30_watermark_retained_source_ready(');
 definition:=replace(definition,needle,'r.status NOT IN(''draft'',''running'',''waiting_review'',''completed'')');
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.v30_watermark_encode_completion_ready(uuid,uuid,text)'::regprocedure);
 needle:='AND kind=''v2.delivery/1'' AND state=''open'';';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_COMPLETION_STATE_DEFINITION_DRIFT';END IF;
 definition:=replace(definition,needle,'AND kind=''v2.delivery/1'' AND state IN(''open'',''closed'');');
 needle:='photo.v30_watermark_pinned_source_ready(';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_COMPLETION_SOURCE_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,'photo.v30_watermark_retained_source_ready(');
 definition:=pg_get_functiondef('photo.v30_delivery_producer_ready(uuid,uuid,uuid,text)'::regprocedure);
 needle:='SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_PRODUCER_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,
 'IF photo.v30_watermark_encode_completion_ready(p_tenant,photo.v30_uuid5(photo.v30_uuid5(r.id,''workflow''),''export.encode:0''),NULL) IS DISTINCT FROM true THEN RETURN false;END IF; '||needle);
END $$;
ALTER FUNCTION photo.v30_watermark_retained_source_ready(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_retained_source_ready(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- Existing function OIDs/owners/ACLs preserved. Ordinary requests keep existing checks.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
