-- +goose Up
-- Outer queues retain exact saved embedded plans and historical omission.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;signature text;failure text;
BEGIN
 definition:=pg_get_functiondef('photo.save_v29_auto_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb,text)'::regprocedure);
 needle:=' SELECT * INTO old FROM photo.runs WHERE id=new_run FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected auto perspective producer';END IF;
 EXECUTE replace(definition,needle,needle||$patch$
 config:=photo.v32_frozen_preview_config(config,old.config_snapshot);
 IF config IS NULL THEN RETURN false;END IF;
$patch$);
 definition:=pg_get_functiondef('photo.read_v29_auto_perspective_replacement_draft(uuid,text,jsonb,text)'::regprocedure);
 needle:='AND r.config_snapshot=expected_config AND r.config_hash=encode(sha256(convert_to(expected_config::text,''UTF8'')),''hex'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected auto perspective reader';END IF;
 EXECUTE replace(definition,needle,$patch$AND r.config_snapshot=photo.v32_frozen_preview_config(expected_config,r.config_snapshot)
 AND r.config_hash=encode(sha256(convert_to(photo.v32_frozen_preview_config(expected_config,r.config_snapshot)::text,'UTF8')),'hex')$patch$);
 FOREACH signature IN ARRAY ARRAY[
 'photo.save_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)',
 'photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:=' SELECT * INTO old FROM photo.runs WHERE id=detector_run FOR UPDATE;';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected detector queue function: %',signature;END IF;
  failure:=CASE WHEN signature LIKE 'photo.save_%' THEN 'false' ELSE 'NULL' END;
  EXECUTE replace(definition,needle,needle||'
 config:=photo.v32_frozen_preview_config(config,old.config_snapshot);
 IF config IS NULL THEN RETURN '||failure||';END IF;');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
