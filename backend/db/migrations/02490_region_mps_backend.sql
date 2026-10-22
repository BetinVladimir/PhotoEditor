-- +goose Up
-- The Region authority body was renamed by 00443; keep its outer qualification gates.
-- Only Region gains MPS; no policy is enabled and no CUDA evidence is reused.
UPDATE photo.operation_registry SET backends='["cuda","mps"]'::jsonb WHERE operation='region.preview' AND version='1';
-- +goose StatementBegin
DO $$ DECLARE name text;source text;anchor text;replacement text;BEGIN
 FOREACH name IN ARRAY ARRAY[
  'photo.v26_region_seal_scope_pre_qualification(uuid,uuid,uuid,uuid,jsonb)',
  'photo.v26_region_descriptor_node(uuid,uuid,uuid,uuid,uuid)',
  'photo.adopt_v2_plan_pre_render_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text)'] LOOP
  source:=pg_get_functiondef(name::regprocedure);
  IF name LIKE '%descriptor%' THEN anchor:='n->''profile''->>''backend'' IS DISTINCT FROM ''cuda''';
  ELSIF name LIKE '%seal_scope%' THEN anchor:='p_node->''profile''->>''backend'' IS DISTINCT FROM ''cuda''';
  ELSE anchor:='node->''profile''->>''backend'' IS DISTINCT FROM ''cuda''';END IF;
  IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'REGION_MPS_SOURCE_DRIFT %',name;END IF;
  replacement:='coalesce('||split_part(anchor,' IS DISTINCT',1)||','''') NOT IN (''cuda'',''mps'')';
  EXECUTE replace(source,anchor,replacement);
 END LOOP;
 source:=pg_get_functiondef('photo.resolve_v26_region_qualification(uuid,uuid,text,text)'::regprocedure);
 anchor:='policy.backend=''cuda''';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'REGION_MPS_RESOLUTION_DRIFT';END IF;
 source:=replace(source,anchor,'policy.backend IN (''cuda'',''mps'')');
 anchor:='op.backends ? ''cuda''';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'REGION_MPS_REGISTRY_DRIFT';END IF;
 EXECUTE replace(source,anchor,'op.backends ? policy.backend');
 source:=pg_get_functiondef('photo.v26_region_qualification_pin_ready(uuid,uuid,text,jsonb)'::regprocedure);
 anchor:='q->>''backend'' IS DISTINCT FROM ''cuda''';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'REGION_MPS_PIN_DRIFT';END IF;
 source:=replace(source,anchor,'coalesce(q->>''backend'','''') NOT IN (''cuda'',''mps'')');
 anchor:='qualified.backend=''cuda''';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'REGION_MPS_REPORT_DRIFT';END IF;
 EXECUTE replace(source,anchor,'qualified.backend=q->>''backend''');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
