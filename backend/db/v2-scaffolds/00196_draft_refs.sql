-- Insert the validated nested artifact closure in the same transaction as a document.
-- +goose StatementBegin
CREATE FUNCTION photo.register_v2_document_refs(p_tenant uuid,p_project uuid,p_document uuid,p_refs jsonb)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref record; n integer:=0;
BEGIN
 IF p_refs IS NULL OR jsonb_typeof(p_refs)<>'array' THEN
  RAISE EXCEPTION 'V2_CLOSURE_INVALID' USING ERRCODE='22023';
 END IF;
 IF jsonb_array_length(p_refs)>512 OR pg_column_size(p_refs)>1048576 THEN
  RAISE EXCEPTION 'V2_CLOSURE_INVALID' USING ERRCODE='22023';
 END IF;
 FOR ref IN SELECT value,ordinality-1 AS ordinal FROM jsonb_array_elements(p_refs) WITH ORDINALITY LOOP
  IF jsonb_typeof(ref.value)<>'object' THEN
   RAISE EXCEPTION 'V2_CLOSURE_INVALID' USING ERRCODE='22023';
  END IF;
  IF (SELECT count(*) FROM jsonb_object_keys(ref.value))<>4 OR
   NOT (ref.value ?& ARRAY['artifact_id','sha256','purpose','ordinal']) OR
   coalesce(ref.value->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
   coalesce(ref.value->>'sha256','') !~ '^[0-9a-f]{64}$' OR
   coalesce(ref.value->>'purpose','') !~ '^[0-9a-f]{64}$' OR
   coalesce(ref.value->>'ordinal','') !~ '^[0-9]{1,3}$' THEN
   RAISE EXCEPTION 'V2_CLOSURE_INVALID' USING ERRCODE='22023';
  END IF;
  IF (ref.value->>'ordinal')::integer<>ref.ordinal THEN
   RAISE EXCEPTION 'V2_CLOSURE_INVALID' USING ERRCODE='22023';
  END IF;
  INSERT INTO photo.v2_document_refs(tenant_id,project_id,document_id,artifact_id,sha256,purpose,ordinal)
  VALUES(p_tenant,p_project,p_document,(ref.value->>'artifact_id')::uuid,
   ref.value->>'sha256',ref.value->>'purpose',ref.ordinal);
  n:=n+1;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v2_document_refs(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_document_refs(uuid,uuid,uuid,jsonb) FROM PUBLIC;
