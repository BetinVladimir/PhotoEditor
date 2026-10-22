"""Immutable geometry replay inventory selected by server authority.

A descriptor is a binding, never proof that a task/result was adopted. Worker
and adopter must separately query live canonical-result authority and verify
all declared physical documents before executing/projection.
"""
from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from photo_worker.contracts.base import Contract,ArtifactRef,SHA256
from photo_worker.contracts.images import TransformStep,Canvas


class RegionBackgroundAsset(Contract):
    portable_asset_id: Annotated[str,Field(min_length=1,max_length=120)]
    style_version_id: UUID
    style: ArtifactRef
    source: ArtifactRef
    expected_bytes: Annotated[int,Field(ge=1,le=536870912)]
    suffix: Literal['.jpg','.jpeg','.png','.tif','.tiff']


class RegionGeometryStage(Contract):
    phase: Literal['background','generate','composition_render'] | None = Field(default=None,exclude_if=lambda value:value is None)
    # Derived from current SQL decision/branch authority; repeated live at
    # worker admission and adoption, never accepted from a public request.
    review_authority: dict | None = Field(default=None,exclude_if=lambda value:value is None)
    operation: Literal['geometry.body','geometry.crop','image.upscale','image.denoise',
        'image.color','image.local','portrait.beauty','portrait.detail','series.balance',
        'sensor.dust_repair','sensor.dust_auto_repair','defects.repair','defects.auto_repair',
        'background.compose','background.blur','image.inpaint','image.generate','image.outpaint']
    operation_version: Literal[1]
    task_id: UUID
    result_id: UUID
    attempt: Annotated[int,Field(ge=1)]
    fencing_token: Annotated[int,Field(ge=1,le=2**53-1)]
    params: ArtifactRef
    source: ArtifactRef
    output: ArtifactRef
    plan: ArtifactRef | None = Field(default=None,exclude_if=lambda value:value is None)
    policy_params: ArtifactRef | None = Field(default=None,exclude_if=lambda value:value is None)
    report: ArtifactRef | None = Field(default=None,exclude_if=lambda value:value is None)
    background_asset: RegionBackgroundAsset | None = Field(default=None,exclude_if=lambda value:value is None)
    background_params: ArtifactRef | None = Field(default=None,exclude_if=lambda value:value is None)
    background_image: ArtifactRef | None = Field(default=None,exclude_if=lambda value:value is None)
    background_canvas: Canvas | None = Field(default=None,exclude_if=lambda value:value is None)
    # Actual immutable appended step from the adopted WorkingImage.
    source_canvas: Canvas
    target_canvas: Canvas
    target_has_alpha: bool = False
    input_mask_count: Annotated[int,Field(ge=0,le=257)] = 0
    transform: TransformStep | None = Field(default=None,exclude_if=lambda value:value is None)


    @model_validator(mode='before')
    @classmethod
    def strict_numbers(cls,value):
        if not isinstance(value,dict) or any(type(value.get(key)) is not int for key in
                ('operation_version','attempt','fencing_token')):
            raise ValueError('REGION_EXECUTION_INTEGER_REQUIRED')
        if 'target_has_alpha' in value and type(value['target_has_alpha']) is not bool:
            raise ValueError('REGION_EXECUTION_ALPHA_BOOLEAN_REQUIRED')
        if 'input_mask_count' in value and type(value['input_mask_count']) is not int:
            raise ValueError('REGION_EXECUTION_MASK_COUNT_INTEGER_REQUIRED')
        return value


class RegionExecutionDescriptor(Contract):
    schema_version: Literal[1]
    source: ArtifactRef
    source_canvas_hash: SHA256
    style: ArtifactRef
    culling_authority: dict | None = Field(default=None,exclude_if=lambda value:value is None)
    stages: Annotated[list[RegionGeometryStage],Field(min_length=1,max_length=128)]

    @model_validator(mode='before')
    @classmethod
    def strict_version(cls,value):
        if not isinstance(value,dict) or type(value.get('schema_version')) is not int:
            raise ValueError('REGION_EXECUTION_VERSION_REQUIRED')
        return value

    @model_validator(mode='after')
    def chain(self):
        current=self.source;tasks=set();results=set();prior=None
        for stage in self.stages:
            if (stage.operation in ('defects.repair','sensor.dust_repair') and stage.review_authority is None
                    or stage.operation not in ('defects.repair','sensor.dust_repair') and stage.review_authority is not None and stage.review_authority.get('route')!='base'):
                raise ValueError('REGION_EXECUTION_REVIEW_AUTHORITY_REQUIRED')
            if (stage.operation=='image.generate' and stage.phase not in ('background','generate')
                    or stage.operation=='image.outpaint' and stage.phase!='composition_render'
                    or stage.operation not in ('image.generate','image.outpaint') and stage.phase is not None):
                raise ValueError('REGION_EXECUTION_PHASE_BINDING_INVALID')
            if ((stage.background_asset is None)!=(stage.background_params is None)
                    or (stage.background_asset is None)!=(stage.background_image is None)
                    or (stage.background_asset is None)!=(stage.background_canvas is None)
                    or stage.background_asset is not None and stage.operation!='background.compose'):
                raise ValueError('REGION_EXECUTION_BACKGROUND_ORIGIN_INVALID')
            if (stage.task_id.int==0 or stage.result_id.int==0 or stage.task_id in tasks
                    or stage.result_id in results or stage.source!=current
                    or stage.output==stage.source or stage.params in (stage.source,stage.output)):
                raise ValueError('REGION_EXECUTION_CHAIN_INVALID')
            if prior is not None and prior!=stage.source_canvas:
                raise ValueError('REGION_EXECUTION_GEOMETRY_DISCONTINUOUS')
            if stage.operation in ('geometry.crop','image.outpaint') and (stage.plan is None or stage.policy_params is None):
                raise ValueError('REGION_EXECUTION_COMPOSITION_POLICY_REQUIRED')
            if stage.operation=='geometry.body' and stage.report is None:
                raise ValueError('REGION_EXECUTION_BODY_REPORT_REQUIRED')
            if stage.transform is None:
                if stage.operation in ('geometry.body','geometry.crop','image.upscale','image.outpaint') or stage.source_canvas!=stage.target_canvas:
                    raise ValueError('REGION_EXECUTION_TRANSFORM_REQUIRED')
            elif stage.transform.from_canvas!=stage.source_canvas or stage.transform.to_canvas!=stage.target_canvas:
                raise ValueError('REGION_EXECUTION_CANVAS_BINDING')
            if stage.transform is not None and stage.operation not in ('geometry.body','geometry.crop','image.upscale','image.outpaint'):
                raise ValueError('REGION_EXECUTION_UNEXPECTED_TRANSFORM')
            if stage.transform is not None and ((stage.operation=='geometry.body' and stage.transform.kind!='dense_warp')
                    or (stage.operation=='geometry.crop' and stage.transform.kind!='matrix')
                    or (stage.operation=='image.upscale' and stage.transform.kind!='resize')
                    or (stage.operation=='image.outpaint' and stage.transform.kind!='matrix')):
                raise ValueError('REGION_EXECUTION_OPERATION_TRANSFORM')
            tasks.add(stage.task_id);results.add(stage.result_id)
            current=stage.output;prior=stage.target_canvas
        return self


def verify_execution_documents(descriptor,source,style_ref,load_document,*,check_active=lambda:None):
    """Independent typed chain verification after live SQL canonical authority.

    load_document reads bounded hash-verified declared JSON bytes. This function
    never upgrades descriptor metadata into canonical task-result authority.
    It returns actual documents for deterministic native replay/projection.
    """
    from photo_worker.contracts.base import canonical_hash,canonical_bytes,parse_json
    from photo_worker.contracts.images import WorkingImage
    if descriptor.style!=style_ref or canonical_hash(source.canvas)!=descriptor.source_canvas_hash:
        raise ValueError('REGION_EXECUTION_SOURCE_INVALID')
    current=source;verified=[]
    for stage in descriptor.stages:
        check_active()
        raw=load_document(stage.params)
        if len(raw)>1048576:raise ValueError('REGION_EXECUTION_PARAMS_TOO_LARGE')
        params=parse_json(raw)
        if (canonical_bytes(params)!=raw or canonical_hash(params)!=stage.params.sha256
                or not isinstance(params,dict) or params.get('image')!=stage.source.model_dump(mode='json')):
            raise ValueError('REGION_EXECUTION_PARAMS_INVALID')
        # A canonical hash authenticates bytes, not schema or semantics. Validate
        # the original operation contract without dumping defaults back into the
        # immutable document (older producers may omit default-valued fields).
        typed_params=execution_params_model(stage.operation).model_validate_json(raw)
        count=0
        if stage.operation=='portrait.detail':count=len(typed_params.regions)+len(typed_params.protected)
        elif stage.operation=='image.local':count=len(typed_params.regions)+sum(region.invert for region in typed_params.regions)+int(typed_params.person is not None)
        elif stage.operation=='sensor.dust_auto_repair':count=len(typed_params.protected)
        elif stage.operation in ('defects.auto_repair','defects.repair'):count=1
        elif stage.operation in ('image.inpaint','image.generate'):count=1+len(typed_params.protected)
        if stage.input_mask_count!=count:raise ValueError('REGION_EXECUTION_MASK_COUNT_MISMATCH')
        raw=load_document(stage.output)
        target=WorkingImage.model_validate_json(raw)
        if canonical_bytes(target)!=raw or canonical_hash(target)!=stage.output.sha256:
            raise ValueError('REGION_EXECUTION_OUTPUT_INVALID')
        if (current.canvas!=stage.source_canvas or target.canvas!=stage.target_canvas or current.color!=target.color
                or stage.target_has_alpha!=(target.alpha is not None)):
            raise ValueError('REGION_EXECUTION_CANVAS_BINDING')
        prefix=current.transform_chain.steps;steps=target.transform_chain.steps
        if stage.transform is None:
            if target.transform_chain!=current.transform_chain:raise ValueError('REGION_EXECUTION_UNDECLARED_TRANSFORM')
        elif len(steps)!=len(prefix)+1 or steps[:-1]!=prefix or steps[-1]!=stage.transform:
            raise ValueError('REGION_EXECUTION_TRANSFORM_BINDING')
        policy={}
        if stage.background_asset is not None:
            from photo_worker.contracts.assets import DecodeParams
            from photo_worker.contracts.background import ComposeParams
            background=ComposeParams.model_validate_json(raw)
            pin=stage.background_asset
            if background.mode!='asset' or background.background is None or background.background!=stage.background_image:
                raise ValueError('REGION_EXECUTION_BACKGROUND_DECODE_BINDING')
            decode_raw=load_document(stage.background_params);decode_json=parse_json(decode_raw)
            decode=DecodeParams.model_validate_json(decode_raw)
            decoded_raw=load_document(background.background);decoded=WorkingImage.model_validate_json(decoded_raw)
            if (len(decode_raw)>1048576 or len(decoded_raw)>1048576
                    or canonical_bytes(decode_json)!=decode_raw or canonical_hash(decode_json)!=stage.background_params.sha256
                    or canonical_bytes(decoded)!=decoded_raw or canonical_hash(decoded)!=background.background.sha256
                    or background.mode!='asset' or pin.style!=descriptor.style or pin.style_version_id.int==0
                    or decode.source!=pin.source or decode.expected_bytes!=pin.expected_bytes or decode.suffix!=pin.suffix
                    or decoded.dtype!='float32' or decoded.alpha is not None or decoded.color!=current.color or decoded.canvas!=stage.background_canvas
                    or decoded.canvas.source_sha256!=pin.source.sha256):
                raise ValueError('REGION_EXECUTION_BACKGROUND_DECODE_BINDING')
            policy['background_image']=decoded
        if params.get('resolved') is not None:
            from photo_worker.contracts.resolved import ResolvedStyle
            resolved_ref=ArtifactRef.model_validate_json(canonical_bytes(params['resolved']))
            resolved_raw=load_document(resolved_ref)
            if len(resolved_raw)>1048576:raise ValueError('REGION_EXECUTION_RESOLVED_TOO_LARGE')
            resolved_json=parse_json(resolved_raw)
            resolved=ResolvedStyle.model_validate_json(resolved_raw)
            if canonical_bytes(resolved_json)!=resolved_raw or canonical_hash(resolved_json)!=resolved_ref.sha256:
                raise ValueError('REGION_EXECUTION_RESOLVED_INVALID')
            # Preserve the actual historical resolution input. It need not be
            # the post-denoise/body stage source, but its style is frozen.
            if resolved.params.style is not None and resolved.params.style!=descriptor.style:
                raise ValueError('REGION_EXECUTION_RESOLVED_STYLE_MISMATCH')
            policy['resolved']=resolved
        if stage.operation in ('geometry.crop','image.outpaint'):
            from photo_worker.contracts.composition import CompositionPlan,CompositionParams
            if stage.plan is None or stage.policy_params is None or stage.operation=='geometry.crop' and params.get('plan')!=stage.plan.model_dump(mode='json'):
                raise ValueError('REGION_EXECUTION_PLAN_BINDING')
            plan_raw=load_document(stage.plan);policy_raw=load_document(stage.policy_params)
            plan=CompositionPlan.model_validate_json(plan_raw);configured=CompositionParams.model_validate_json(policy_raw)
            if (canonical_bytes(plan)!=plan_raw or canonical_hash(plan)!=stage.plan.sha256
                    or canonical_bytes(configured)!=policy_raw or canonical_hash(configured)!=stage.policy_params.sha256
                    or plan.image!=stage.source or configured.structure!=plan.structure):
                raise ValueError('REGION_EXECUTION_COMPOSITION_POLICY_BINDING')
            policy.update(plan=plan,params=configured)
        verified.append((stage,params,current,target,policy));current=target
    check_active()
    return tuple(verified)


def execution_params_model(operation):
    from photo_worker.contracts.body import BodyParams
    from photo_worker.contracts.composition import CropParams
    from photo_worker.contracts.restoration import DenoiseParams,UpscaleParams,InpaintParams,GenerationParams,OutpaintParams
    from photo_worker.contracts.image_color import ColorParams
    from photo_worker.contracts.image_local import LocalParams
    from photo_worker.contracts.beauty import BeautyParams
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.contracts.background import ComposeParams,BlurParams
    from photo_worker.contracts.defects import DefectRepairParams
    from photo_worker.contracts.dust import DustRepairParams
    from photo_worker.contracts.conservative import DefectAutoRepairParams,DustAutoRepairParams
    models={
        'geometry.body':BodyParams,'geometry.crop':CropParams,
        'image.denoise':DenoiseParams,'image.upscale':UpscaleParams,
        'image.color':ColorParams,'image.local':LocalParams,
        'portrait.beauty':BeautyParams,'portrait.detail':PortraitParams,
        'background.compose':ComposeParams,'background.blur':BlurParams,
        'defects.repair':DefectRepairParams,'defects.auto_repair':DefectAutoRepairParams,
        'sensor.dust_repair':DustRepairParams,'sensor.dust_auto_repair':DustAutoRepairParams,
        'image.inpaint':InpaintParams,'image.generate':GenerationParams,'image.outpaint':OutpaintParams,
    }
    if operation not in models:
        raise ValueError('REGION_EXECUTION_OPERATION_SCHEMA_UNAVAILABLE')
    return models[operation]
