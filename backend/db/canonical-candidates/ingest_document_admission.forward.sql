-- Forward-only candidate; unnumbered, outside embedded migration set.
-- Restores the scoped ingest owner alternative lost by migration00090.
-- Not an installer or permission to adopt unknown lineage. Apply only after review/tests.
DO $patch$
DECLARE target record; old_meta jsonb; new_meta jsonb; body text; definition text; before_hash text;
 needle text := $needle$AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner)) THEN$needle$;
 replacement text := $replacement$AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner) AND NOT (r.kind='ingest' AND NOT r.graph_sealed AND EXISTS(SELECT FROM photo.ingest_node_plans n WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.id=p_owner AND n.params_sha=p_sha))) THEN$replacement$;
BEGIN
 FOR target IN SELECT * FROM (VALUES ('photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text)','30e7df9e6dfaf7f2f2cb856b816bfea3d2b9194990e78ebc3404c700b4d01d9f','0f72635dec322fe3876660eaf5856279d3176dccdbd176bf818866e1638621d1'),
('photo.complete_backend_document_pre_v28_donor(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)','30a3e521bb6bf489db5a1480788aeba10ea60ab18378c695fd1ec4b5022aeaed','50bd22c7cbe5aabd7ce6832ea9ff48c5a071bc68419fb790b3916cbde39c5ee7')) AS x(signature,old_sha,next_sha) LOOP
  SELECT to_jsonb(p),p.prosrc,pg_get_functiondef(p.oid),encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')
   INTO STRICT old_meta,body,definition,before_hash FROM pg_proc p WHERE p.oid=target.signature::regprocedure;
  IF before_hash<>target.old_sha OR old_meta->>'prokind'<>'f' OR old_meta->>'prosecdef'<>'true'
   OR pg_get_userbyid((old_meta->>'proowner')::oid)<>'photo_fn_owner'
   OR (length(body)-length(replace(body,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'INGEST_DOCUMENT_ADMISSION_PREDECESSOR_MISMATCH' USING ERRCODE='55000';
  END IF;
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'INGEST_DOCUMENT_ADMISSION_DEFINITION_MISMATCH' USING ERRCODE='55000';
  END IF;
  EXECUTE replace(definition,needle,replacement);
  SELECT to_jsonb(p),encode(sha256(convert_to(p.prosrc,'UTF8')),'hex') INTO STRICT new_meta,before_hash
   FROM pg_proc p WHERE p.oid=target.signature::regprocedure;
  IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' OR before_hash<>target.next_sha THEN
   RAISE EXCEPTION 'INGEST_DOCUMENT_ADMISSION_METADATA_CHANGED' USING ERRCODE='55000';
  END IF;
 END LOOP;
END
$patch$;
