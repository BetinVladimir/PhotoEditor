-- +goose Up
-- Keep replacement draft bytes immutable; queue is an outer config field.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.save_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text)'::regprocedure);
 needle:=' SELECT * INTO old FROM photo.runs WHERE id=new_run FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected negative replacement producer';END IF;
 EXECUTE replace(definition,needle,needle||$patch$
 config:=photo.v32_frozen_preview_config(config,old.config_snapshot);
 IF config IS NULL THEN RETURN false;END IF;
$patch$);
 definition:=pg_get_functiondef('photo.read_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)'::regprocedure);
 needle:='(SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(r.config_snapshot) k)=ARRAY[''project_id'',''v29_negative_replacement'',''v29_negative_replacement_draft_jcs'']::text[]';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected negative replacement reader';END IF;
 EXECUTE replace(definition,needle,$patch$
 (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(r.config_snapshot-'queue_class') k)=ARRAY['project_id','v29_negative_replacement','v29_negative_replacement_draft_jcs']::text[]
 AND (NOT(r.config_snapshot ? 'queue_class') OR r.config_snapshot->'queue_class'='"interactive"'::jsonb)
$patch$);
 definition:=pg_get_functiondef('photo.probe_v29_negative_replacement_publication(uuid,uuid,uuid,uuid,text)'::regprocedure);
 needle:='jsonb_build_object(''project_id'',p_project,''v29_negative_replacement'',marker,''v29_negative_replacement_draft_jcs'',p_expected)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected negative replacement probe';END IF;
 EXECUTE replace(definition,needle,'photo.v32_frozen_preview_config('||needle||',r.config_snapshot)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
