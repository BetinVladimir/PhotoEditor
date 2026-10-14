"""Canonical depth origin plus exact original producer Params replay."""
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.person import PersonParams
from photo_worker.runtime.executor import OperationFailure
from .local_manual_admission import _attempt_pin


class SQLRelightPersonAuthority:
    def __init__(self,database,artifacts,*,objects=None,bucket=None):
        if (not callable(getattr(database,'relight_person_origin',None))
                or not callable(getattr(artifacts,'contract_bytes',None))):
            raise ValueError('RELIGHT_PERSON_AUTHORITY_INVALID')
        self.database,self.artifacts=database,artifacts
        if objects is not None and (not callable(getattr(objects,'get_object',None))
                or not isinstance(bucket,str) or not bucket):
            raise ValueError('RELIGHT_PERSON_AUTHORITY_INVALID')
        self.objects,self.bucket=objects,bucket

    def _read_original(self,record,check_active):
        from contextlib import closing
        if (record['bucket']!=self.bucket or record['media_type']!='application/json'
                or type(record['bytes']) is not int or not 2<=record['bytes']<=65536
                or not isinstance(record['object_key'],str) or not record['object_key']
                or not isinstance(record['object_version'],str) or record['object_version'] in ('','null')):
            raise OperationFailure('RELIGHT_PERSON_ORIGIN_INVALID')
        check_active()
        response=self.objects.get_object(self.bucket,record['object_key'],version_id=record['object_version'])
        try:
            with closing(response):
                raw=bytearray()
                while len(raw)<=record['bytes']:
                    check_active();block=response.read(min(65536,record['bytes']+1-len(raw)))
                    if not isinstance(block,bytes):raise OperationFailure('RELIGHT_PERSON_STREAM_INVALID')
                    if not block:break
                    raw.extend(block)
                if len(raw)!=record['bytes']:raise OperationFailure('RELIGHT_PERSON_SIZE_CHANGED')
                return bytes(raw)
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()

    def verify(self,ctx,params,source,subject):
        ctx.check_active();attempt=_attempt_pin(ctx)
        body=canonical_bytes(params);mask_body=canonical_bytes(subject)
        if canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        def lookup():
            ctx.check_active()
            record=self.database.relight_person_origin(ctx.lease,body,mask_body)
            ctx.check_active()
            if _attempt_pin(ctx)!=attempt:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
            if not isinstance(record,dict) or set(record)!={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}:
                raise OperationFailure('RELIGHT_PERSON_ORIGIN_INVALID')
            return record
        original=lookup()
        ref=ArtifactRef.model_validate_json(canonical_bytes({key:original[key] for key in ('artifact_id','sha256')}))
        if ref not in ctx.lease.command.inputs:raise OperationFailure('RELIGHT_PERSON_INPUT_UNDECLARED')
        raw=(self._read_original(original,ctx.check_active) if self.objects is not None
             else self.artifacts.contract_bytes(ref,PersonParams))
        if not isinstance(raw,bytes) or not 2<=len(raw)<=65536:
            raise OperationFailure('RELIGHT_PERSON_PARAMS_INVALID')
        producer=PersonParams.model_validate_json(raw)
        from hashlib import sha256
        if sha256(raw).hexdigest()!=ref.sha256 or producer.image!=params.base.source:
            raise OperationFailure('RELIGHT_PERSON_SOURCE_MISMATCH')
        if lookup()!=original:raise OperationFailure('RELIGHT_PERSON_ORIGIN_CHANGED')
        return True
