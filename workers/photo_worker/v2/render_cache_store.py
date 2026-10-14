"""Scoped primary manifest adapter for version-pinned cached closure reads."""
from .adoption import AdoptionInvalid,ArtifactRef,ArtifactEvidence
from .adoption_store import PostgresObjectEvidenceReader,_Location
from .render_cache_identity import render_cache_context,render_cache_identity
from photo_worker.contracts.base import canonical_bytes,parse_json


class RenderCacheObjectReader(PostgresObjectEvidenceReader):
    def __init__(self,connection,objects,*,tenant_id,task_id,attempt,fence,allowed_bucket,inputs,candidate):
        super().__init__(connection,objects,tenant_id=tenant_id,task_id=task_id,
                         attempt=attempt,fence=fence,allowed_bucket=allowed_bucket)
        profile=inputs.execution_profile()
        key=render_cache_identity(tenant_id,inputs.recipe,profile,inputs.parent_roots,inputs.qualification_pin)
        body=canonical_bytes(render_cache_context(tenant_id,inputs.recipe,profile,
                                                inputs.parent_roots,inputs.qualification_pin)).decode()
        inventory=parse_json(candidate.inventory_jcs,max_bytes=16777216)
        rows=connection.execute('SELECT * FROM photo.v26_render_cache_artifacts(%s,%s,%s,%s,%s,%s)',
                                (tenant_id,task_id,attempt,fence,key,body)).fetchall()
        if len(rows)!=len(inventory) or not rows:
            raise AdoptionInvalid('RENDER_CACHE_MANIFEST_STALE')
        for ordinal,(row,entry) in enumerate(zip(rows,inventory)):
            if not isinstance(row,(tuple,list)) or len(row)!=9:
                raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            identifier,digest,size,media,role,bucket,object_key,version,index=row
            ref=ArtifactRef(identifier,digest)
            if (str(identifier)!=entry['artifact_id'] or digest!=entry['sha256'] or index!=ordinal
                    or type(size) is not int or size<1 or bucket!=allowed_bucket
                    or not isinstance(object_key,str) or not object_key
                    or not isinstance(version,str) or version in ('','null')):
                raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            location=_Location(bucket,object_key,version,ArtifactEvidence(ref,media,size,role,'cache'))
            if ref in self._locations and self._locations[ref]!=location:
                raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            self._locations[ref]=location
        if candidate.root!=ArtifactRef(rows[0][0],rows[0][1]):
            raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')

    def _location(self,ref):
        try:return self._locations[ref]
        except KeyError as error:raise AdoptionInvalid('RENDER_CACHE_UNDECLARED_ARTIFACT') from error

    def staged_outputs(self):
        raise AdoptionInvalid('RENDER_CACHE_NOT_OUTPUT_AUTHORITY')
