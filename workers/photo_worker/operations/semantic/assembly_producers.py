"""Bounded exact-version producer Params reader under CPU assembly authority."""
from copy import deepcopy
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams,EditIntent,TransferCandidate,SemanticTransferParams
from photo_worker.runtime.parameters import read_contract
from photo_worker.v2.adoption import AdoptionInvalid
from .provenance import verify_candidate_bindings


def sql_semantic_assembly_provenance(database,objects,bucket,ctx):
    if (not callable(getattr(database,'semantic_assembly_producer',None))
            or not callable(getattr(objects,'get_object',None)) or not isinstance(bucket,str) or not 3<=len(bucket)<=63):
        raise ValueError('SEMANTIC_PRODUCER_AUTHORITY_REQUIRED')
    lease=deepcopy(ctx.lease)
    def active():
        ctx.check_active()
        if ctx.lease!=lease:raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
    def scope(ref):
        active();row=database.semantic_assembly_producer(deepcopy(lease),deepcopy(ref))
        if not isinstance(row,dict):raise AdoptionInvalid('SEMANTIC_PRODUCER_SCOPE_STALE')
        row=deepcopy(row)
        try:
            for key in ('producer_task_id','producer_result_id','params_artifact_id'):UUID(str(row[key]))
            for key in ('producer_attempt','producer_fence'):
                if type(row[key]) is not int or row[key]<1:raise ValueError()
            ArtifactRef(artifact_id=UUID(str(row['params_artifact_id'])),sha256=row['params_sha'])
            if (row['bucket']!=bucket or not isinstance(row['object_key'],str) or not row['object_key']
                    or not isinstance(row['object_version'],str) or row['object_version'] in ('','null')
                    or type(row['bytes']) is not int or not 2<=row['bytes']<=1048576):raise ValueError()
        except (ValueError,KeyError,TypeError) as error:
            raise AdoptionInvalid('SEMANTIC_PRODUCER_EVIDENCE_INVALID') from error
        return row
    def verify(assembly,intent,snapshot,candidates):
        active()
        assembly=V2SemanticAssembleParams.model_validate_json(canonical_bytes(assembly))
        intent=EditIntent.model_validate_json(canonical_bytes(intent))
        if not isinstance(candidates,(list,tuple)) or len(candidates)!=len(assembly.candidates):
            raise AdoptionInvalid('SEMANTIC_PRODUCER_COVERAGE_INVALID')
        owned=[TransferCandidate.model_validate_json(canonical_bytes(value)) for value in candidates]
        pins=[]
        for ref,candidate in zip(assembly.candidates,owned):
            pin=scope(ref);pins.append((ref,pin))
            params_ref=ArtifactRef(artifact_id=UUID(str(pin['params_artifact_id'])),sha256=pin['params_sha'])
            class Stream:
                def __init__(self):
                    active();self.response=objects.get_object(bucket,pin['object_key'],version_id=pin['object_version'])
                    self.bytes=0;self.closed=False
                def read(self,size):
                    active();block=self.response.read(size)
                    if isinstance(block,bytes):self.bytes+=len(block)
                    return block
                def close(self):
                    if not self.closed:
                        self.closed=True
                        try:self.response.close()
                        finally:self.response.release_conn()
            streams=[]
            def open_stream(_):
                stream=Stream();streams.append(stream);return stream
            transfer=read_contract(params_ref,SemanticTransferParams,open_stream,active,max_bytes=pin['bytes'])
            if len(streams)!=1 or streams[0].bytes!=pin['bytes']:raise AdoptionInvalid('SEMANTIC_PRODUCER_BYTES_INVALID')
            if scope(ref)!=pin:raise AdoptionInvalid('SEMANTIC_PRODUCER_SCOPE_STALE')
            verify_candidate_bindings(assembly,intent,ref,candidate,transfer,check_active=active)
        for ref,pin in pins:
            if scope(ref)!=pin:raise AdoptionInvalid('SEMANTIC_PRODUCER_SCOPE_STALE')
        active()
    return verify
