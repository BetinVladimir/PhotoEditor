"""Executable asset operations using task-scoped authenticated artifacts."""
from __future__ import annotations
from pathlib import Path
from pydantic import ValidationError
from photo_worker.contracts.assets import VerifyParams,InspectParams,DecodeParams,VerifiedAsset,AssetMetadata
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .verify import verify_decode,verify_magic,verify_size,verify_suffix,VerifiedSource
from .inspect import read_metadata
from .decode import icc_to_work,RAWThreadControlError
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain


def _execute(ctx:ExecutionContext,artifacts:TaskArtifacts,verify:bool)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:params=artifacts.params(VerifyParams if verify else InspectParams)
    except ValidationError as exc:raise OperationFailure('INPUT_INVALID') from exc
    if params.source not in ctx.lease.command.inputs:raise OperationFailure('INPUT_INVALID')
    ctx.progress(0,3)
    with artifacts.input(params.source,suffix=params.suffix) as local:
        ctx.progress(1,3)
        try:
            if verify:source=verify_decode(local.path,params.expected_bytes,params.source.sha256,check_active=ctx.check_active)
            else:
                size,sha=verify_size(local.path,params.expected_bytes,params.source.sha256,check_active=ctx.check_active)
                kind=verify_magic(local.path);verify_suffix(kind,local.path.suffix)
                source=VerifiedSource(local.path,kind,size,sha)
            ctx.check_active()
            metadata=read_metadata(source)
            # Bind inspected metadata to the same immutable source bytes.
            verify_size(local.path,source.bytes,source.sha256,check_active=ctx.check_active)
        except ValueError as exc:
            # ICC/format/size failures are deterministic input errors; do not
            # expose source paths or decoder exception text in task diagnostics.
            raise OperationFailure('INPUT_INVALID') from exc
        ctx.progress(2,3)
        manifest=AssetMetadata(source=params.source,width=metadata.width,height=metadata.height,
            bit_depth=metadata.bit_depth,has_alpha=metadata.has_alpha,orientation=metadata.orientation,
            icc_sha256=metadata.icc_sha256,format=metadata.format,sensor_alignment=metadata.sensor_alignment,
            captured_at=metadata.captured_at,captured_local=metadata.captured_local)
        if verify:
            output=artifacts.stage_contract(VerifiedAsset(source=params.source,bytes=source.bytes,metadata=manifest),'verified')
        else:output=artifacts.stage_contract(manifest,'metadata')
        ctx.progress(3,3)
        return OperationResult(outputs=[output],bytes_read=source.bytes)


def _decode(ctx:ExecutionContext,artifacts:TaskArtifacts,registry:ICCRegistry)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:params=artifacts.params(DecodeParams)
    except ValidationError as exc:raise OperationFailure('INPUT_INVALID') from exc
    if params.source not in ctx.lease.command.inputs:raise OperationFailure('INPUT_INVALID')
    ctx.progress(0,4)
    with artifacts.input(params.source,suffix=params.suffix) as local:
        try:
            size,sha=verify_size(local.path,params.expected_bytes,params.source.sha256,check_active=ctx.check_active)
            kind=verify_magic(local.path);verify_suffix(kind,local.path.suffix)
            source=VerifiedSource(local.path,kind,size,sha)
            metadata=read_metadata(source)
        except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
        ctx.progress(1,4)
        # Header allowance includes both NPY headers. Temporary publication uses
        # hard links, so it never duplicates pixel storage on this filesystem.
        budget=metadata.width*metadata.height*(14 if metadata.has_alpha else 12)+1048576
        with artifacts.scratch.reserve(budget) as reservation:
            pixels=reservation.planned_path('working-rgb','.npy')
            alpha=reservation.planned_path('alpha','.npy') if metadata.has_alpha else None
            try:
                decoded=icc_to_work(source,pixels,registry,params.assume_profile,analyze=params.analyze,
                    tile=params.tile,alpha_output=alpha,raw_development=params.raw_development,check_active=ctx.check_active)
            except RAWThreadControlError as exc:raise OperationFailure('CONFIG_INVALID') from exc
            except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
            ctx.progress(2,4)
            pixel_ref=artifacts.stage(pixels,reservation,'working_rgb','application/octet-stream')
            alpha_ref=artifacts.stage(alpha,reservation,'alpha','application/octet-stream') if alpha else None
            written=pixels.stat().st_size+(alpha.stat().st_size if alpha else 0)
            manifest=WorkingImage(pixels=pixel_ref,
                canvas=Canvas(width=decoded.width,height=decoded.height,orientation=1,
                    space_id='oriented:'+sha,source_sha256=sha),
                color=ColorSpace(profile_id=decoded.profile_id,icc_sha256=decoded.profile_sha256,encoding='gamma24'),
                dtype='float32',alpha=alpha_ref,transform_chain=TransformChain.create([]))
            ctx.progress(3,4)
        # Pixel uploads are immutable; release local files before manifest allocation.
        manifest_ref=artifacts.stage_contract(manifest,'working_image')
        ctx.progress(4,4)
        return OperationResult(outputs=[pixel_ref,*([alpha_ref] if alpha_ref else []),manifest_ref],bytes_read=size,
            bytes_written=written+len(canonical_bytes(manifest)))


def asset_operations(config:ArtifactConfig,icc_root:Path|None=None)->list[Operation]:
    def verified(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'verified'})) as artifacts:
            return _execute(ctx,artifacts,True)
    def inspected(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'metadata'})) as artifacts:
            return _execute(ctx,artifacts,False)
    def decoded(ctx):
        if icc_root is None:raise OperationFailure('CONFIG_INVALID')
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','alpha','working_image'})) as artifacts:
            return _decode(ctx,artifacts,ICCRegistry(icc_root))
    operations=[Operation('asset.verify','media',verified),Operation('asset.inspect','cpu',inspected)]
    if icc_root is not None:operations.append(Operation('asset.decode','cpu',decoded))
    return operations
