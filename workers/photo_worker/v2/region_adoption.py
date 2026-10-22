"""Region result binding and pinned encoded-byte proof before SQL adoption."""
from photo_worker.operations.learning.contracts import FrozenRecipe
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.result import RegionDerivativeManifest, verify_region_encoded
from .adoption import AdoptionInvalid
from .lens_bypass import _document, _ref


def verify_region_result(reader, params_ref, root_ref, *, target_icc_sha256,
                         check_active=lambda: None,scratch_root=None,max_bytes=None):
    """Target ICC is trusted runtime evidence, never inferred from output JSON."""
    if (not isinstance(target_icc_sha256, str) or len(target_icc_sha256) != 64
            or any(c not in '0123456789abcdef' for c in target_icc_sha256)):
        raise AdoptionInvalid('REGION_ADOPTION_ICC_REQUIRED')
    check_active()
    params = _document(reader, params_ref, RegionPreviewParams, 'input')
    check_active()
    manifest = _document(reader, root_ref, RegionDerivativeManifest, 'output')
    check_active()
    recipe = _document(reader, _ref(params.recipe.ref), FrozenRecipe, 'input')
    check_active()
    if recipe != params.frozen_recipe:
        raise AdoptionInvalid('REGION_ADOPTION_RECIPE_INVALID')
    source_ref = _ref(params.source.ref)
    source_evidence = reader.evidence(source_ref)
    check_active()
    if (source_evidence.ref != source_ref or source_evidence.source != 'input'
            or source_evidence.media_type != 'application/json'
            or source_evidence.bytes != params.source.bytes):
        raise AdoptionInvalid('REGION_ADOPTION_SOURCE_INVALID')
    source_body = reader.read_json(source_ref, 1048576)
    check_active()
    try:
        source = params.validate_source(source_body)
        manifest.validate_for(params, target_icc_sha256, source.alpha is not None if params.execution is None else params.execution.stages[-1].target_has_alpha)
    except ValueError as exc:
        raise AdoptionInvalid('REGION_ADOPTION_BINDING_INVALID') from exc
    if manifest.projection is not None:
        _verify_projected_geometry(reader,params,source,manifest,scratch_root,max_bytes,check_active)
    derivative_ref = _ref(manifest.derivative)
    evidence = reader.evidence(derivative_ref)
    check_active()
    if (evidence.ref != derivative_ref or evidence.source != 'output'
            or evidence.role != 'derivative' or evidence.media_type != manifest.media_type
            or evidence.bytes != manifest.bytes):
        raise AdoptionInvalid('REGION_ADOPTION_OUTPUT_INVALID')
    stream = getattr(reader, 'iter_bytes', None)
    if not callable(stream):
        raise AdoptionInvalid('REGION_ADOPTION_STREAM_REQUIRED')
    check_active()
    iterator = iter(stream(derivative_ref, manifest.bytes))
    body = bytearray()
    stream_failed = False
    try:
        for block in iterator:
            check_active()
            if (not isinstance(block, bytes) or not block or len(block) > 65536
                    or len(body) + len(block) > manifest.bytes):
                raise AdoptionInvalid('REGION_ADOPTION_STREAM_INVALID')
            body.extend(block)
    except BaseException:
        stream_failed = True
        raise
    finally:
        close = getattr(iterator, 'close', None)
        if close is not None:
            try:
                close()
            except BaseException:
                if not stream_failed:
                    raise
    try:
        verify_region_encoded(manifest, params, bytes(body),
            target_sha256=target_icc_sha256, expected_alpha=source.alpha is not None if params.execution is None else params.execution.stages[-1].target_has_alpha)
    except (ValueError, OSError) as exc:
        raise AdoptionInvalid('REGION_ADOPTION_BYTES_INVALID') from exc
    check_active()


def _verify_projected_geometry(reader,params,source,manifest,scratch_root,max_bytes,check_active):
    from contextlib import contextmanager
    import math
    from photo_worker.operations.region.execution import verify_execution_documents
    from photo_worker.operations.region.projection import ProjectedBounds,project_region_chain
    from photo_worker.operations.region.contracts import RegionGeometry
    from .array_evidence import ArrayEvidenceSession
    if scratch_root is None or type(max_bytes) is not int or max_bytes<1:
        raise AdoptionInvalid('REGION_PROJECTION_PHYSICAL_BUDGET_REQUIRED')
    def read(ref):
        check_active();evidence=reader.evidence(_ref(ref))
        if evidence.source!='input' or evidence.ref!=_ref(ref) or evidence.media_type!='application/json':
            raise AdoptionInvalid('REGION_PROJECTION_DECLARED_DOCUMENT_REQUIRED')
        body=reader.read_json(_ref(ref),1048576);check_active();return body
    try:
        verified=verify_execution_documents(params.execution,source,params.frozen_recipe.base_style,read,
            check_active=check_active)
        from photo_worker.contracts.style import StyleV2
        from photo_worker.operations.learning.recipe import materialize_frozen_style
        from photo_worker.operations.region.geometry_policy import validate_region_geometry_policy
        from photo_worker.contracts.base import canonical_bytes,canonical_hash
        base_raw=read(params.frozen_recipe.base_style);base=StyleV2.model_validate_json(base_raw)
        if canonical_bytes(base)!=base_raw or canonical_hash(base)!=params.frozen_recipe.base_style.sha256:
            raise ValueError('REGION_PROJECTION_STYLE_INVALID')
        effective=materialize_frozen_style(params.frozen_recipe,base,params.frozen_recipe.base_style,
            params.frozen_recipe.source,params.frozen_recipe.icc_lock_sha,params.frozen_recipe.model_lock_sha)
        from photo_worker.operations.region.collection_selection import validate_collection_selection
        validate_collection_selection(effective,params.execution)
        from photo_worker.operations.region.native_order import validate_native_region_order
        validate_native_region_order(effective,verified,model_lock_sha=params.frozen_recipe.model_lock_sha,original_style=base)
        from photo_worker.operations.region.conservative import conservative_documents
        repairs=conservative_documents(effective,verified,read)
        from photo_worker.operations.region.reviewed import reviewed_documents
        repairs+=reviewed_documents(effective,verified,read)
        from photo_worker.operations.region.inpaint import inpaint_entry
        inpaint=inpaint_entry(effective,verified,params.frozen_recipe.model_lock_sha)
        from photo_worker.operations.region.generation import generation_entry
        generations=[entry for phase in ('background','generate')
            if (entry:=generation_entry(effective,verified,phase,params.frozen_recipe.model_lock_sha)) is not None]
        bodies,geometry_steps,_=validate_region_geometry_policy(effective,verified,model_lock_sha=params.frozen_recipe.model_lock_sha)
        from photo_worker.operations.region.detail import detail_entry
        detail=detail_entry(effective,verified)
        from photo_worker.operations.region.background import background_entry
        background=background_entry(effective,verified)
        from photo_worker.operations.region.color_inputs import local_evidence,series_correction
        local=local_evidence(effective,verified)
        if local is not None:series_correction(effective,local[0],read)
        if background is not None and background[0].background_asset is not None:
            from hashlib import sha256
            pin=background[0].background_asset;ref=_ref(pin.source);evidence=reader.evidence(ref)
            if (evidence.source!='input' or evidence.ref!=ref or evidence.bytes!=pin.expected_bytes
                    or evidence.media_type not in ('image/jpeg','image/png','image/tiff')):
                raise ValueError('REGION_BACKGROUND_SOURCE_EVIDENCE_INVALID')
            digest=sha256();count=0;blocks=iter(reader.iter_bytes(ref,pin.expected_bytes))
            try:
                for block in blocks:
                    check_active()
                    if not isinstance(block,bytes) or not 1<=len(block)<=65536 or count+len(block)>pin.expected_bytes:
                        raise ValueError('REGION_BACKGROUND_SOURCE_STREAM_INVALID')
                    count+=len(block);digest.update(block)
            finally:
                close=getattr(blocks,'close',None)
                if close is not None:close()
            if count!=pin.expected_bytes or digest.hexdigest()!=pin.source.sha256:
                raise ValueError('REGION_BACKGROUND_SOURCE_BYTES_INVALID')
        target=verified[-1][3];projection=manifest.projection
        if projection.target_canvas!=target.canvas or projection.target_transform_chain!=target.transform_chain:
            raise ValueError('REGION_PROJECTION_TARGET_INVALID')
        with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
            for geometry_stage,_,geometry_source,_ in geometry_steps:
                if geometry_stage.operation=='image.outpaint':
                    shape=(geometry_source.canvas.height,geometry_source.canvas.width)
                    arrays.array(_ref(geometry_source.pixels),(*shape,3),source='input',unit=True,legacy_input_npy=True)
                    if geometry_source.alpha is not None:
                        arrays.array(_ref(geometry_source.alpha),shape,source='input',unit=True,legacy_alpha=True,legacy_input_npy=True)
            for generation_params,generation_source in generations:
                shape=(generation_source.canvas.height,generation_source.canvas.width)
                arrays.array(_ref(generation_source.pixels),(*shape,3),source='input',unit=True,legacy_input_npy=True)
                if generation_source.alpha is not None:
                    arrays.array(_ref(generation_source.alpha),shape,source='input',unit=True,legacy_alpha=True,legacy_input_npy=True)
                for mask in [generation_params.mask,*generation_params.protected]:
                    arrays.array(_ref(mask.artifact),shape,source='input',unit=True,legacy_input_npy=True)
            if inpaint is not None:
                inpaint_params,inpaint_source=inpaint
                shape=(inpaint_source.canvas.height,inpaint_source.canvas.width)
                arrays.array(_ref(inpaint_source.pixels),(*shape,3),source='input',unit=True,legacy_input_npy=True)
                if inpaint_source.alpha is not None:
                    arrays.array(_ref(inpaint_source.alpha),shape,source='input',unit=True,legacy_alpha=True,legacy_input_npy=True)
                for mask in [inpaint_params.mask,*inpaint_params.protected]:
                    arrays.array(_ref(mask.artifact),shape,source='input',unit=True,legacy_input_npy=True)
            for repair_source,repair_masks,labels in repairs:
                shape=(repair_source.canvas.height,repair_source.canvas.width)
                arrays.array(_ref(repair_source.pixels),(*shape,3),source='input',unit=True,legacy_input_npy=True)
                if repair_source.alpha is not None:
                    arrays.array(_ref(repair_source.alpha),shape,source='input',unit=True,legacy_alpha=True,legacy_input_npy=True)
                for mask in repair_masks:
                    arrays.array(_ref(mask.artifact),shape,source='input',unit=True,legacy_input_npy=True)
                if labels is not None:
                    arrays.array(_ref(labels),shape,source='input',legacy_input_npy=True,labels=True)
            if local is not None:
                local_params,local_source=local
                for region in local_params.regions:
                    arrays.array(_ref(region.mask.artifact),(local_source.canvas.height,local_source.canvas.width),source='input',unit=True,legacy_input_npy=True)
            if background is not None:
                _,background_params,background_source,_=background
                arrays.array(_ref(background_params.subject.artifact),(background_source.canvas.height,background_source.canvas.width),source='input',unit=True,legacy_input_npy=True)
                from photo_worker.contracts.background import BlurParams
                if isinstance(background_params,BlurParams):
                    arrays.array(_ref(background_params.depth.artifact),(background_source.canvas.height,background_source.canvas.width),source='input',unit=True,legacy_input_npy=True)
                elif background_params.mode=='asset':
                    image=next(item[4]['background_image'] for item in verified if item[0]==background[0])
                    arrays.array(_ref(image.pixels),(image.canvas.height,image.canvas.width,3),source='input',unit=True,legacy_input_npy=True)
            if detail is not None:
                _,detail_params,detail_source,_,_=detail
                for mask in [*(region.mask for region in detail_params.regions),*detail_params.protected]:
                    arrays.array(_ref(mask.artifact),(detail_source.canvas.height,detail_source.canvas.width),source='input',unit=True,legacy_input_npy=True)
            for _,_,_,_,policy in verified:
                resolved=policy.get('resolved');lut=resolved.absolute_lut if resolved is not None else None
                if lut is not None:
                    arrays.array(_ref(lut.artifact),(lut.size,lut.size,lut.size,3),source='input',unit=True,legacy_input_npy=True,lut=True)
            @contextmanager
            def field(ref,canvas):
                check_active()
                yield arrays.array(_ref(ref),(canvas.height,canvas.width,2),source='input',inverse_field=True,legacy_input_npy=True)
                check_active()
            # Independent physical safety evaluation; a descriptor/report alone
            # is never evidence that the dense field preserves protected areas.
            from photo_worker.contracts.body import BodyWarpReport
            from photo_worker.algorithms.body_warp import validate_field
            import numpy as np
            for stage,body_params,before,after in bodies:
                report_raw=read(stage.report);report=BodyWarpReport.model_validate_json(report_raw)
                if (canonical_bytes(report)!=report_raw or canonical_hash(report)!=stage.report.sha256
                        or report.image!=body_params.image or report.poses!=body_params.poses
                        or report.person!=body_params.person.artifact or report.faces!=body_params.faces
                        or report.requested_strength!=body_params.strength or report.effective_strength>body_params.strength):
                    raise ValueError('REGION_PROJECTION_BODY_REPORT_INVALID')
                dense=arrays.array(_ref(stage.transform.inverse_field),(before.canvas.height,before.canvas.width,2),source='input',inverse_field=True,legacy_input_npy=True)
                person=arrays.array(_ref(body_params.person.artifact),(before.canvas.height,before.canvas.width),source='input',unit=True,legacy_input_npy=True)
                safety=validate_field(dense,body_params.max_displacement_image_fraction*max(before.canvas.height,before.canvas.width),person,check_active=check_active)
                if (safety.max_displacement!=report.metrics.max_displacement_pixels
                        or safety.min_jacobian!=report.metrics.min_jacobian
                        or safety.protected_center_displacement!=report.metrics.protected_center_displacement_pixels
                        or report.applied and not safety.safe):raise ValueError('REGION_PROJECTION_BODY_SAFETY_INVALID')
                if not report.applied:
                    for y in range(0,before.canvas.height,256):
                        check_active();yy,xx=np.mgrid[y:min(y+256,before.canvas.height),:before.canvas.width]
                        if not np.array_equal(dense[y:y+256,...,0],xx) or not np.array_equal(dense[y:y+256,...,1],yy):
                            raise ValueError('REGION_PROJECTION_BODY_IDENTITY_INVALID')
            original=params.geometry
            bounds=project_region_chain(ProjectedBounds(original.left,original.top,original.width,original.height),
                source.canvas,target.canvas,source.transform_chain,target.transform_chain,
                load_inverse_field=field,check_active=check_active)
            scale=min(1.,params.request.max_edge/max(bounds.width,bounds.height))
            expected=RegionGeometry(left=bounds.left,top=bounds.top,width=bounds.width,height=bounds.height,
                output_width=max(1,math.floor(bounds.width*scale)),output_height=max(1,math.floor(bounds.height*scale)))
            if projection.geometry!=expected:raise ValueError('REGION_PROJECTION_GEOMETRY_INVALID')
    except (ValueError,OSError) as error:
        raise AdoptionInvalid('REGION_PROJECTION_INVALID') from error
    check_active()
