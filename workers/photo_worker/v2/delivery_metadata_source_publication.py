"""Canonical publication envelope shared with the Go metadata source compiler.

Caller supplies API-authorized preparation and original MIME from authoritative
catalog evidence. This compiler cannot grant ACL or publish an artifact.
"""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from .delivery_metadata_original_service import PreparedAuthorizedMetadataOriginal
from .delivery_metadata_freeze import verify_frozen_delivery_metadata
from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES


def compile_metadata_source_publication(prepared,original_mime,policy,*,approved_fields=()):
    if (not isinstance(prepared,PreparedAuthorizedMetadataOriginal)
            or original_mime not in ('image/jpeg','image/png','image/tiff') and original_mime not in RAW_METADATA_FILETYPES):
        raise ValueError('DELIVERY_METADATA_PUBLICATION_INPUT')
    scope=prepared.scope;value=prepared.prepared
    binding=VersionBinding.model_validate_json(prepared.binding_jcs)
    if (canonical_bytes(binding)!=prepared.binding_jcs or binding.asset_version_id!=scope.version
            or binding.source.artifact_id!=scope.source.artifact_id or binding.source.sha256!=scope.source.sha256
            or value.source.original_jcs!=canonical_bytes({'artifact_id':str(scope.original.artifact_id),'sha256':scope.original.sha256})
            or type(value.source.original_bytes) is not int or not 1<=value.source.original_bytes<=2*1024**3):
        raise ValueError('DELIVERY_METADATA_PUBLICATION_SCOPE')
    ref=ArtifactRef.model_validate_json(value.metadata_ref_jcs)
    if (canonical_bytes(ref)!=value.metadata_ref_jcs or ref.artifact_id==scope.original.artifact_id
            or value.source.metadata_sha256!=ref.sha256):
        raise ValueError('DELIVERY_METADATA_PUBLICATION_SOURCE')
    frozen=verify_frozen_delivery_metadata(value.frozen_jcs,value.frozen_sha256,binding,ref,
        value.source.metadata_jcs,policy=policy,approved_fields=approved_fields)
    raw=canonical_bytes({'schema_version':1,'scope':{
        'tenant_id':str(scope.tenant),'project_id':str(scope.project),'actor_id':str(scope.actor),
        'session_id':str(scope.session),'binding':binding.model_dump(mode='json'),
        'original':{'artifact_id':str(scope.original.artifact_id),'sha256':scope.original.sha256},
        'original_bytes':value.source.original_bytes,'original_mime':original_mime},
        'metadata_source':ref.model_dump(mode='json'),'metadata_jcs':value.source.metadata_jcs.decode(),
        'policy':policy,'approved_fields':frozen.approved_fields,
        'frozen_jcs':value.frozen_jcs.decode(),'frozen_sha256':value.frozen_sha256})
    if len(raw)>1048576:raise ValueError('DELIVERY_METADATA_PUBLICATION_SIZE')
    return raw,sha256(raw).hexdigest()
