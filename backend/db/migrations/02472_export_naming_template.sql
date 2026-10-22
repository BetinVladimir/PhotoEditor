-- +goose Up
-- Frozen preset names are expanded before the existing flat-name sanitizer and
-- deterministic collision resolver. The format extension remains authoritative.
-- +goose StatementBegin
CREATE FUNCTION photo.download_export_basename(p_original text,p_sha text,p_ordinal bigint,p_template text)
RETURNS text LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE template text:=coalesce(nullif(p_template,''),'{original_name}');base text;
BEGIN
 IF length(template)>200 OR template ~ '[{}]' AND regexp_replace(template,'\{(original_name|name|sha8|ordinal)\}','','g') ~ '[{}]'
 OR template ~ '[/\\[:cntrl:]]' OR btrim(template)='' THEN RAISE EXCEPTION 'EXPORT_NAMING_TEMPLATE_INVALID' USING ERRCODE='22023';END IF;
 base:=regexp_replace(p_original,'\.[^.]*$','');
 RETURN replace(replace(replace(replace(template,'{original_name}',base),'{name}',base),'{sha8}',left(p_sha,8)),'{ordinal}',lpad(p_ordinal::text,4,'0'));
END $$;
ALTER FUNCTION photo.download_export_basename(text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_basename(text,text,bigint,text) FROM PUBLIC;
DO $$ DECLARE definition text;
 anchor text:=$old$regexp_replace(coalesce(nullif(si.payload->>'filename',''),u.filename,'photo-'||lpad(p.ordinal::text,4,'0')),'\.[^.]*$','')$old$;
 replacement text:=$new$photo.download_export_basename(coalesce(nullif(si.payload->>'filename',''),u.filename,'photo-'||lpad(p.ordinal::text,4,'0')),a.sha256,p.ordinal,j.preset->'config'->>'naming_template')$new$;
BEGIN
 SELECT pg_get_functiondef('photo.download_encode_outcomes(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'EXPORT_NAMING_SOURCE_DRIFT';END IF;
 EXECUTE replace(definition,anchor,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
