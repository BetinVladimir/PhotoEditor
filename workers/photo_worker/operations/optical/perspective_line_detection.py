"""CPU evidence producer; installation must supply trusted working ICC."""
from contextlib import ExitStack
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.contracts.images import WorkingImage,DenseWarpTransform
from photo_worker.contracts.optical_perspective import LineSegments
from photo_worker.contracts.perspective_line_detection import PerspectiveLineDetectionParams,PerspectiveLineDetectionResult
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure,OperationResult
from .perspective_line_evidence import prepare_detected_perspective_lines
from .perspective_line_resources import admit_line_detection


def execute(ctx,artifacts,*,working_icc_sha):
    ctx.check_active()
    profile=ctx.lease.execution_profile
    if not isinstance(profile,dict) or profile.get('backend')!='cpu' or profile.get('precision')!='float32':
        raise OperationFailure('CONFIG_INVALID')
    if (not isinstance(working_icc_sha,str) or len(working_icc_sha)!=64 or
            any(c not in '0123456789abcdef' for c in working_icc_sha)):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(PerspectiveLineDetectionParams)
        params=PerspectiveLineDetectionParams.model_validate_json(canonical_bytes(params))
        raw=artifacts.contract_bytes(params.base.source,WorkingImage)
        parse_json(raw,1048576)
        source=WorkingImage.model_validate_json(raw)
        if (sha256(raw).hexdigest()!=params.base.source.sha256 or canonical_bytes(source)!=raw or
                source.dtype!='float32' or source.color.icc_sha256!=working_icc_sha):
            raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
        if canonical_hash(source.canvas)!=params.base.canvas_hash:
            raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
        expected=[params.base.source,source.pixels,*([source.alpha] if source.alpha else [])]
        if len({r.artifact_id for r in expected})!=len(expected):
            raise ValueError('PERSPECTIVE_LINE_INPUTS_INVALID')
        if artifacts.declared_contract_refs('WorkingImage',512)!=(params.base.source,):
            raise ValueError('PERSPECTIVE_LINE_INPUTS_INVALID')
        arrays=[source.pixels,*([source.alpha] if source.alpha else [])]
        primary_ids={r.artifact_id for r in expected}
        field_refs={}
        for step in source.transform_chain.steps:
            if not isinstance(step,DenseWarpTransform):continue
            ref=step.inverse_field
            if ref.artifact_id in primary_ids or (ref.artifact_id in field_refs and field_refs[ref.artifact_id]!=ref):
                raise ValueError('PERSPECTIVE_LINE_INPUTS_INVALID')
            field_refs[ref.artifact_id]=ref
        arrays.extend(field_refs.values())
        required={(r.artifact_id,r.sha256) for r in arrays}
        declared=artifacts.declared_contract_refs('NpyArray',512)
        if {(r.artifact_id,r.sha256) for r in declared}!=required:
            raise ValueError('PERSPECTIVE_LINE_INPUTS_INVALID')
        admit_line_detection(profile,source.canvas.width,source.canvas.height,source.alpha is not None)
        shape=(source.canvas.height,source.canvas.width)
        # The input stream verifier enforces declared original object SHA/size.
        with ExitStack() as inputs:
            rgb_path=inputs.enter_context(artifacts.input(source.pixels,suffix='.npy')).path
            rgb=inputs.enter_context(read_array(rgb_path,(*shape,3),'float32',ctx.check_active,unit_interval=False))
            alpha=None
            if source.alpha is not None:
                path=inputs.enter_context(artifacts.input(source.alpha,suffix='.npy')).path
                alpha=inputs.enter_context(read_alpha_array(path,shape,ctx.check_active))
            evidence=prepare_detected_perspective_lines(raw,params.base.source.sha256,rgb,alpha,
                working_icc_sha=working_icc_sha,check_active=ctx.check_active)
        lines=None if evidence.lines is None else LineSegments.model_validate_json(evidence.lines)
        result=PerspectiveLineDetectionResult(schema_version=1,base=params.base,
            status='unsupported' if lines is None else 'available',lines=lines)
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active()
    ref=artifacts.stage_contract(result,'document')
    return OperationResult(outputs=[ref],review_required=True,bytes_written=len(canonical_bytes(result)))


def perspective_line_detection_operations(config:ArtifactConfig,*,working_icc_sha):
    if (not isinstance(working_icc_sha,str) or len(working_icc_sha)!=64 or
            any(c not in '0123456789abcdef' for c in working_icc_sha)):
        raise ValueError('PERSPECTIVE_LINE_INSTALLATION_INVALID')
    def run(ctx):
        command=ctx.lease.command
        if (command.operation!='optical.perspective_lines' or command.operation_version!='1' or
                command.resource_class!='cpu'):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active()
        with TaskArtifacts.create(ctx,config,frozenset({'document'})) as artifacts:
            return execute(ctx,artifacts,working_icc_sha=working_icc_sha)
    return [Operation('optical.perspective_lines','cpu',run)]
