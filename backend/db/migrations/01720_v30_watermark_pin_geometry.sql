-- +goose Up
-- Value checks complement current source ACL; they do not prove physical NPY bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_pin_geometry(p_body jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;canvas jsonb;binding jsonb;item jsonb;w bigint;h bigint;n integer:=0;minimum bigint;maximum bigint;size bigint;
BEGIN
 IF jsonb_typeof(p_body->'binding') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(p_body->'binding'))<>3
 OR jsonb_typeof(p_body->'manifest_jcs') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 binding:=p_body->'binding';image:=(p_body->>'manifest_jcs')::jsonb;canvas:=image->'canvas';
 IF jsonb_typeof(canvas) IS DISTINCT FROM 'object'
 OR jsonb_typeof(canvas->'width') IS DISTINCT FROM 'number'
 OR jsonb_typeof(canvas->'height') IS DISTINCT FROM 'number'
 OR (canvas->>'width') !~ '^[1-9][0-9]{0,5}$'
 OR (canvas->>'height') !~ '^[1-9][0-9]{0,5}$'
 OR canvas->'orientation' IS DISTINCT FROM '1'::jsonb
 OR coalesce(binding->>'canvas_hash','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_body->'arrays') IS DISTINCT FROM 'array' OR jsonb_array_length(p_body->'arrays')<>2
 THEN RETURN false;END IF;
 w:=(canvas->>'width')::bigint;h:=(canvas->>'height')::bigint;
 IF w>100000 OR h>100000 OR w*h>200000000
 OR image->'pixels'->>'artifact_id' IS NOT DISTINCT FROM image->'alpha'->>'artifact_id'
 OR image->'pixels'->>'artifact_id' IS NOT DISTINCT FROM binding->'source'->>'artifact_id'
 OR image->'alpha'->>'artifact_id' IS NOT DISTINCT FROM binding->'source'->>'artifact_id' THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_body->'arrays') LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(item))<>3
  OR NOT(item ?& ARRAY['contract','ref','bytes']) OR jsonb_typeof(item->'bytes') IS DISTINCT FROM 'number'
  OR coalesce(item->>'bytes','') !~ '^[1-9][0-9]{0,10}$' THEN RETURN false;END IF;
  maximum:=w*h*CASE n WHEN 0 THEN 12 ELSE 4 END;
  minimum:=w*h*CASE n WHEN 0 THEN 12 ELSE 2 END;
  size:=(item->>'bytes')::bigint;
  IF size<minimum OR size>maximum+8192 THEN RETURN false;END IF;
  n:=n+1;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_pin_geometry(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_pin_geometry(jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text:='body:=p_raw::jsonb;scope:=body->''scope'';';
BEGIN
 SELECT pg_get_functiondef('photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text)'::regprocedure) INTO definition;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark pin geometry patch mismatch';END IF;
 EXECUTE replace(definition,needle,needle||' IF photo.v30_watermark_pin_geometry(body) IS DISTINCT FROM true THEN RETURN false;END IF;');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
