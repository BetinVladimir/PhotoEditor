"""API-authorized original object reader; never grants scheduler source access.

Connection must authenticate as photo_api. Canvas/WorkingImage bytes and metadata
approval remain separate admission obligations. Every access repeats live SQL ACL.
"""
from dataclasses import dataclass
from uuid import UUID
from .adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from .adoption_store import PostgresObjectEvidenceReader,_Location
from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES


@dataclass(frozen=True)
class MetadataOriginalScope:
    tenant: UUID
    project: UUID
    version: UUID
    source: ArtifactRef
    actor: UUID
    session: UUID
    original: ArtifactRef

    def __post_init__(self):
        if any(not isinstance(value,UUID) or value.int==0 for value in
                (self.tenant,self.project,self.version,self.actor,self.session)):
            raise ValueError('DELIVERY_METADATA_ORIGINAL_SCOPE')
        for ref in (self.source,self.original):
            if not isinstance(ref,ArtifactRef) or ref.artifact_id.int==0:
                raise ValueError('DELIVERY_METADATA_ORIGINAL_SCOPE')
            ArtifactRef.parse({'artifact_id':str(ref.artifact_id),'sha256':ref.sha256})
        if self.source.artifact_id==self.original.artifact_id:
            raise ValueError('DELIVERY_METADATA_ORIGINAL_ALIAS')


class DeliveryMetadataOriginalReader(PostgresObjectEvidenceReader):
    def __init__(self,connection,objects,*,scope,allowed_bucket,check_active=lambda:None):
        if (connection.autocommit is not True or not isinstance(scope,MetadataOriginalScope)
                or not isinstance(allowed_bucket,str) or not allowed_bucket or not callable(check_active)):
            raise ValueError('DELIVERY_METADATA_ORIGINAL_READER_INSTALLATION')
        self.connection,self.objects,self.scope=connection,objects,scope
        self.allowed_bucket,self.active=allowed_bucket,check_active
        self._locations={}

    def _location(self,ref):
        self.active()
        if ref!=self.scope.original:raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_UNDECLARED')
        s=self.scope
        with self.connection.transaction():
            self.connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(s.tenant),))
            self.connection.execute("SELECT set_config('statement_timeout','30000',true)")
            row=self.connection.execute(
                'SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type '
                'FROM photo.v30_delivery_metadata_original(%s,%s,%s,%s,%s,%s,%s)',
                (s.tenant,s.project,s.version,s.source.artifact_id,s.source.sha256,s.actor,s.session)).fetchone()
        if (row is None or row[0]!=ref.artifact_id or row[1]!=ref.sha256
                or type(row[2]) is not int or not 1<=row[2]<=2**53-1
                or row[3]!=self.allowed_bucket or not isinstance(row[4],str) or not row[4] or '\x00' in row[4]
                or not isinstance(row[5],str) or row[5] in ('','null')
                or len(row[5].encode('utf-8'))>1024 or any(c in row[5] for c in ('\r','\n','\x00'))
                or row[6] not in ('image/jpeg','image/png','image/tiff') and row[6] not in RAW_METADATA_FILETYPES):
            raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_AUTHORITY')
        current=_Location(row[3],row[4],row[5],ArtifactEvidence(ref,row[6],row[2],None,'input'))
        previous=self._locations.setdefault(ref,current)
        if current!=previous:raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_REPLACED')
        self.active();return current

    def iter_bytes(self,ref,max_bytes):
        iterator=super().iter_bytes(ref,max_bytes)
        try:
            for block in iterator:
                self.active();yield block
            self._location(ref)
            self.active()
        finally:iterator.close()

    def read_json(self,*args):raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_NOT_JSON')
    def staged_outputs(self):raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_NOT_RESULT')
