-- +goose Up
-- Independent placement admission matches Go/worker binary64 ties-to-even.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_placement_ready(w bigint,h bigint,mw bigint,mh bigint,preset jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scale double precision;a double precision;b double precision;tw bigint;th bigint;mx bigint;my bigint;x bigint;y bigint;
BEGIN
 IF w IS NULL OR h IS NULL OR mw IS NULL OR mh IS NULL OR w NOT BETWEEN 1 AND 100000 OR h NOT BETWEEN 1 AND 100000
 OR mw NOT BETWEEN 1 AND 100000 OR mh NOT BETWEEN 1 AND 100000 OR w*h>200000000 OR mw*mh>200000000
 OR jsonb_typeof(preset) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF jsonb_typeof(preset->'margin_x') IS DISTINCT FROM 'number' OR jsonb_typeof(preset->'margin_y') IS DISTINCT FROM 'number' THEN RETURN false;END IF;
 mx:=(preset->>'margin_x')::bigint;my:=(preset->>'margin_y')::bigint;
 IF mx NOT BETWEEN 0 AND w OR my NOT BETWEEN 0 AND h THEN RETURN false;END IF;
 scale:=least(1::double precision,w::double precision*0.25/mw::double precision);
 a:=mw::double precision*scale;b:=mh::double precision*scale;
 tw:=greatest(1,(CASE WHEN a-floor(a)<0.5 THEN floor(a) WHEN a-floor(a)>0.5 THEN floor(a)+1 WHEN mod(floor(a)::bigint,2)=0 THEN floor(a) ELSE floor(a)+1 END)::bigint);
 th:=greatest(1,(CASE WHEN b-floor(b)<0.5 THEN floor(b) WHEN b-floor(b)>0.5 THEN floor(b)+1 WHEN mod(floor(b)::bigint,2)=0 THEN floor(b) ELSE floor(b)+1 END)::bigint);
 x:=mx;y:=my;
 CASE preset->>'anchor'
 WHEN 'top_left' THEN NULL;
 WHEN 'top_right' THEN x:=w-tw-mx;
 WHEN 'bottom_left' THEN y:=h-th-my;
 WHEN 'bottom_right' THEN x:=w-tw-mx;y:=h-th-my;
 WHEN 'center' THEN x:=(w-tw)/2;y:=(h-th)/2;
 ELSE RETURN false;
 END CASE;
 RETURN x>=0 AND y>=0 AND x+tw<=w AND y+th<=h;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR division_by_zero THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_placement_ready(bigint,bigint,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_placement_ready(bigint,bigint,bigint,bigint,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE signature text;definition text;needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.v30_watermark_initial_stage_scope(uuid,uuid,integer,text,text)','photo.v30_watermark_after_presentation_scope(uuid,uuid,integer,text,text)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:=' IF params IS DISTINCT FROM expected THEN RETURN NULL;END IF;';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_PLACEMENT_SCOPE_DRIFT: %',signature;END IF;
  definition:=replace(definition,needle,needle||E'\n IF photo.v30_watermark_placement_ready(w::bigint,h::bigint,mw::bigint,mh::bigint,cfg->''watermark'') IS DISTINCT FROM true THEN RETURN NULL;END IF;');
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
