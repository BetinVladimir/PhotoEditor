"""Compose independent live source, producer and qualification authorities."""
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.relight_qualification_loader import load_relight_qualification
from .local_manual_admission import _attempt_pin
from .relight_inputs import verify_relight_inputs


class RelightProducerResolver:
    """Each call replays all producers and reloads current qualification bytes.

    Mask verifiers are explicit (selector, protected-role) entries. No fallback
    treats an unknown producer as manual or person. Additional qualified mask
    producers can be installed without changing the relight algorithm.
    """
    def __init__(self,database,objects,bucket,runtime_sha,depth_authority,mask_authorities):
        if (not callable(getattr(database,'relight_qualification_evidence',None))
                or not callable(getattr(objects,'get_object',None))
                or not isinstance(bucket,str) or not bucket
                or not isinstance(runtime_sha,str) or len(runtime_sha)!=64
                or any(c not in '0123456789abcdef' for c in runtime_sha)
                or not callable(getattr(depth_authority,'verify',None))
                or not isinstance(mask_authorities,dict) or not mask_authorities
                or any(not isinstance(key,tuple) or len(key)!=2 or not isinstance(key[0],str)
                       or type(key[1]) is not bool or not callable(verifier)
                       for key,verifier in mask_authorities.items())):
            raise ValueError('RELIGHT_AUTHORITY_INVALID')
        self.database,self.objects,self.bucket=database,objects,bucket
        self.runtime_sha,self.depth=runtime_sha,depth_authority
        self.masks=dict(mask_authorities)

    def resolve(self,ctx,params,source,depth,subject,protected,calibration):
        ctx.check_active()
        def identity():
            return (_attempt_pin(ctx),canonical_hash([ref.model_dump(mode='json') for ref in ctx.lease.command.inputs]))
        attempt=identity()
        if canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        verify_relight_inputs(params,source,depth,subject,protected,calibration,ctx.lease.command.inputs)
        verifiers=[]
        for mask,role in ((subject,False),(protected,True)):
            verifier=self.masks.get((mask.selector,role))
            if verifier is None:raise OperationFailure('RELIGHT_MASK_AUTHORITY_UNAVAILABLE')
            verifiers.append((verifier,mask))
        def checked(call):
            ctx.check_active()
            if call() is not True:raise OperationFailure('RELIGHT_PRODUCER_NOT_READY')
            ctx.check_active()
            if identity()!=attempt:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
        checked(lambda:self.depth.verify(ctx,params,source,depth))
        for verifier,mask in verifiers:
            checked(lambda:verifier(ctx,params,source,mask))
        report,calibration_raw,policy,current_pin=load_relight_qualification(self.database,ctx.lease,
            canonical_bytes(params),self.objects,self.bucket,expected_runtime_sha=self.runtime_sha,
            check_active=ctx.check_active)
        pin=current_pin()
        ctx.check_active()
        if identity()!=attempt:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
        return pin,report,calibration_raw,policy


def qualified_relight_operations(config,database,objects,bucket,runtime_lock,expected_runtime_sha):
    """Explicit installation; caller still owns qualified deployment/registry."""
    from .relight_adapter import relight_operations
    from .relight_admission import QualifiedRelightAdmission,LeaseGatedRelightResolver
    from .relight_depth_authority import SQLRelightDepthAuthority
    from .relight_person_authority import SQLRelightPersonAuthority
    from .relight_manual_authority import RelightManualMaskAuthority,SQLRelightManualOrigin
    def admission(ctx,artifacts):
        ctx.check_active()
        depth=SQLRelightDepthAuthority(database,artifacts,objects=objects,bucket=bucket)
        person=SQLRelightPersonAuthority(database,artifacts,objects=objects,bucket=bucket)
        manual=RelightManualMaskAuthority(artifacts,SQLRelightManualOrigin(database,artifacts))
        masks={('person',False):person.verify,
            ('manual',False):lambda *args:manual.verify(*args,protected=False),
            ('manual',True):lambda *args:manual.verify(*args,protected=True)}
        resolver=RelightProducerResolver(database,objects,bucket,expected_runtime_sha,depth,masks)
        return QualifiedRelightAdmission(LeaseGatedRelightResolver(database,resolver),expected_runtime_sha)
    return relight_operations(config,admission,runtime_lock=runtime_lock,expected_runtime_sha=expected_runtime_sha)
