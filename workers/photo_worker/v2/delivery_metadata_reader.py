"""Version-pinned object IO for metadata adoption; authority is supplied separately.

The authority adapter must reread the current tenant/task/attempt/fence/result
on every call and must not leave a database transaction open across object IO.
This reader is not itself SQL adoption or permission to dispatch metadata.write.
"""
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID
from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from .adoption_store import PostgresObjectEvidenceReader, _Location


@dataclass(frozen=True)
class MetadataResultScope:
    tenant_id: UUID
    task_id: UUID
    attempt: int
    fence: int
    result_id: UUID

    def __post_init__(self):
        if (any(not isinstance(value, UUID) or value.int==0
                for value in (self.tenant_id,self.task_id,self.result_id))
                or type(self.attempt) is not int or not 1<=self.attempt<=2147483647
                or type(self.fence) is not int or not 1<=self.fence<=9223372036854775807):
            raise ValueError('DELIVERY_METADATA_READER_SCOPE')


@dataclass(frozen=True)
class MetadataObjectEvidence:
    scope: MetadataResultScope
    evidence: ArtifactEvidence
    bucket: str
    key: str
    version: str


class MetadataResultAuthority(Protocol):
    def location(self, scope: MetadataResultScope, ref: ArtifactRef) -> MetadataObjectEvidence: ...
    def outputs(self, scope: MetadataResultScope) -> tuple[ArtifactRef, ...]: ...


class DeliveryMetadataEvidenceReader(PostgresObjectEvidenceReader):
    # Reuse only bounded, version-pinned object IO; no inherited SQL queries.
    def __init__(self, authority, objects, *, scope, allowed_bucket, check_active=lambda:None):
        if (not isinstance(scope,MetadataResultScope) or not isinstance(allowed_bucket,str)
                or not allowed_bucket or not callable(check_active)):
            raise ValueError('DELIVERY_METADATA_READER_INSTALLATION')
        self.authority,self.objects,self.scope=authority,objects,scope
        self.allowed_bucket,self.active=allowed_bucket,check_active
        self._locations={}

    def _location(self, ref):
        self.active()
        if not isinstance(ref,ArtifactRef) or ref.artifact_id.int==0:
            raise AdoptionInvalid('DELIVERY_METADATA_READER_REF')
        ArtifactRef.parse({'artifact_id':str(ref.artifact_id),'sha256':ref.sha256})
        value=self.authority.location(self.scope,ref)
        if (not isinstance(value,MetadataObjectEvidence) or value.scope!=self.scope
                or not isinstance(value.evidence,ArtifactEvidence) or value.evidence.ref!=ref
                or value.bucket!=self.allowed_bucket
                or not isinstance(value.key,str) or not value.key or '\x00' in value.key
                or not isinstance(value.version,str) or value.version in ('','null')
                or len(value.version.encode('utf-8'))>1024
                or any(c in value.version for c in ('\r','\n','\x00'))
                or type(value.evidence.bytes) is not int or not 1<=value.evidence.bytes<=2**53-1
                or value.evidence.source not in ('input','output')
                or not isinstance(value.evidence.media_type,str) or not value.evidence.media_type):
            raise AdoptionInvalid('DELIVERY_METADATA_READER_EVIDENCE')
        current=_Location(value.bucket,value.key,value.version,value.evidence)
        previous=self._locations.setdefault(ref,current)
        if previous!=current:raise AdoptionInvalid('DELIVERY_METADATA_READER_CHANGED')
        self.active()
        return current

    def staged_outputs(self):
        self.active();refs=self.authority.outputs(self.scope)
        if not isinstance(refs,tuple) or len(refs)!=3 or any(not isinstance(ref,ArtifactRef) for ref in refs):
            raise AdoptionInvalid('DELIVERY_METADATA_READER_OUTPUTS')
        for ref in refs:
            if ref.artifact_id.int==0:raise AdoptionInvalid('DELIVERY_METADATA_READER_OUTPUTS')
            ArtifactRef.parse({'artifact_id':str(ref.artifact_id),'sha256':ref.sha256})
            if self._location(ref).evidence.source!='output':
                raise AdoptionInvalid('DELIVERY_METADATA_READER_OUTPUTS')
        if len({ref.artifact_id for ref in refs})!=3:
            raise AdoptionInvalid('DELIVERY_METADATA_READER_OUTPUTS')
        self.active();return set(refs)

    def iter_bytes(self, ref, max_bytes):
        iterator=super().iter_bytes(ref,max_bytes)
        try:
            for block in iterator:
                self.active()
                yield block
            self._location(ref)
            self.active()
        finally:
            iterator.close()

    def read_json(self, ref, max_bytes):
        if type(max_bytes) is not int or not 2<=max_bytes<=1048576:
            raise AdoptionInvalid('DELIVERY_METADATA_READER_JSON_LIMIT')
        if self.evidence(ref).media_type!='application/json':
            raise AdoptionInvalid('DELIVERY_METADATA_READER_JSON_MEDIA')
        return b''.join(self.iter_bytes(ref,max_bytes))
