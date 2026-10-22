"""Task-private native geometry replay over current adopted stage evidence."""
from contextlib import contextmanager,ExitStack
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.body import BodyWarpReport
from photo_worker.runtime.arrays import read_inverse_field,read_array
from photo_worker.algorithms.body_warp import validate_field
from .execution import verify_execution_documents
from .geometry_policy import validate_region_geometry_policy
from .geometry_native import GeometryPixels,prepare_region_body,prepare_region_crop,prepare_region_upscale


class RegionGeometryReplay:
    def __init__(self,artifacts,params,source,effective,*,backend,model_lock_sha,upscale_provider=None,generation_pool=None,profile=None,verify_runtime=None,check_active=lambda:None):
        self.artifacts=artifacts;self.params=params;self.source=source
        self.backend=backend;self.model_lock_sha=model_lock_sha;self.upscale_provider=upscale_provider
        self.generation_pool=generation_pool;self.profile=profile;self.verify_runtime=verify_runtime
        self.check_active=check_active
        if params.execution is None:raise ValueError('REGION_EXECUTION_REQUIRED')
        def document(ref):
            check_active();artifacts.require_declared_input(ref)
            with artifacts.input(ref,suffix='.json') as local:
                if local.path.stat().st_size>1048576:raise ValueError('REGION_EXECUTION_DOCUMENT_TOO_LARGE')
                raw=local.path.read_bytes()
            check_active();return raw
        self.document=document
        self.verified=verify_execution_documents(params.execution,source,params.frozen_recipe.base_style,document,check_active=check_active)
        from photo_worker.contracts.style import StyleV2
        base_raw=document(params.frozen_recipe.base_style)
        self.base_style=StyleV2.model_validate_json(base_raw)
        if canonical_bytes(self.base_style)!=base_raw or canonical_hash(self.base_style)!=params.frozen_recipe.base_style.sha256:
            raise ValueError('REGION_EXECUTION_STYLE_INVALID')
        from .native_order import validate_native_region_order
        validate_native_region_order(effective,self.verified,model_lock_sha=model_lock_sha,original_style=self.base_style)
        from .conservative import conservative_documents
        conservative_documents(effective,self.verified,document)
        from .reviewed import reviewed_documents
        reviewed_documents(effective,self.verified,document)
        from .inpaint import inpaint_entry
        inpaint_entry(effective,self.verified,model_lock_sha)
        from .generation import generation_entry
        for phase in ('background','generate'):generation_entry(effective,self.verified,phase,model_lock_sha)
        for stage,_,_,_,_ in self.verified:
            if stage.background_asset is not None:
                pin=stage.background_asset
                artifacts.require_declared_input(pin.source)
                with artifacts.input(pin.source,suffix=pin.suffix) as local:
                    check_active()
                    if local.path.stat().st_size!=pin.expected_bytes:
                        raise ValueError('REGION_BACKGROUND_SOURCE_BYTES_MISMATCH')
        # Resolved external LUTs remain dependencies even when native replay
        # recomputes effective color. Authenticate their physical shape/dtype and
        # finite unit range; a JSON grid declaration alone is insufficient.
        verify_region_luts(self.verified,artifacts,check_active=check_active)
        self.body,self.crop,self.upscale=validate_region_geometry_policy(effective,self.verified,model_lock_sha=model_lock_sha)
        operations=[entry[0].operation for entry in self.verified]
        from .detail import detail_entry
        from .background import background_entry
        background_entry(effective,self.verified)
        from .color_inputs import local_evidence,series_correction
        local=local_evidence(effective,self.verified)
        if local is not None:series_correction(effective,local[0],self.document)
        detail=detail_entry(effective,self.verified)
        if detail and any(op not in ('defects.auto_repair','defects.repair','background.compose','background.blur','image.inpaint','image.generate','image.outpaint','geometry.crop','image.upscale') for op in operations[operations.index('portrait.detail')+1:]):
            raise ValueError('REGION_DETAIL_STAGE_ORDER_INVALID')
        # Native ordering matches Apply. Geometry cannot be arbitrarily moved
        # across a frozen color/local/portrait stage.
        if self.body and any(op not in ('image.denoise',) for op in operations[:operations.index('geometry.body')]):
            raise ValueError('REGION_BODY_STAGE_ORDER_INVALID')
        if self.crop and any(op not in ('image.upscale',) for op in operations[operations.index(self.crop[0][0].operation)+1:]):
            raise ValueError('REGION_CROP_STAGE_ORDER_INVALID')
        if self.upscale and operations[-1]!='image.upscale':raise ValueError('REGION_UPSCALE_STAGE_ORDER_INVALID')

    @contextmanager
    def before_color(self,rgb,alpha):
        if not self.body:
            yield GeometryPixels(rgb,alpha,self.source.canvas,self.source.transform_chain);return
        stage,params,source,target=self.body[0]
        report_raw=self.document(stage.report);report=BodyWarpReport.model_validate_json(report_raw)
        if (canonical_bytes(report)!=report_raw or canonical_hash(report)!=stage.report.sha256
                or report.image!=params.image or report.poses!=params.poses or report.person!=params.person.artifact
                or report.faces!=params.faces or report.requested_strength!=params.strength
                or report.effective_strength>params.strength):raise ValueError('REGION_BODY_REPORT_BINDING_INVALID')
        with ExitStack() as scopes:
            field_local=scopes.enter_context(self.artifacts.input(stage.transform.inverse_field,suffix='.npy'))
            field=scopes.enter_context(read_inverse_field(field_local.path,(source.canvas.height,source.canvas.width),self.check_active))
            mask_local=scopes.enter_context(self.artifacts.input(params.person.artifact,suffix='.npy'))
            person=scopes.enter_context(read_array(mask_local.path,rgb.shape[:2],'float32',self.check_active,unit_interval=True))
            validation=validate_field(field,params.max_displacement_image_fraction*max(rgb.shape[:2]),person,check_active=self.check_active)
            if (validation.max_displacement!=report.metrics.max_displacement_pixels
                    or validation.min_jacobian!=report.metrics.min_jacobian
                    or validation.protected_center_displacement!=report.metrics.protected_center_displacement_pixels
                    or report.applied and not validation.safe):raise ValueError('REGION_BODY_SAFETY_MISMATCH')
            if not report.applied:
                require_skipped_body_identity(field,check_active=self.check_active)
            with prepare_region_body(source,target,report,rgb,alpha,field,self.artifacts.scratch,check_active=self.check_active) as result:
                yield result;self.check_active()

    @contextmanager
    def person_mask(self,original_mask,guide):
        if not self.body:
            yield original_mask;return
        stage,params,source,target=self.body[0]
        if params.person.canvas!=stage.transform.from_canvas:
            raise ValueError('REGION_BODY_PERSON_CANVAS_INVALID')
        from photo_worker.operations.mask.transform_prepare import prepare_transform
        with ExitStack() as scopes:
            local=scopes.enter_context(self.artifacts.input(params.person.artifact,suffix='.npy'))
            person=scopes.enter_context(read_array(local.path,original_mask.shape,'float32',self.check_active,unit_interval=True))
            require_person_mask_identity(person,original_mask,check_active=self.check_active)
            local_field=scopes.enter_context(self.artifacts.input(stage.transform.inverse_field,suffix='.npy'))
            field=scopes.enter_context(read_inverse_field(local_field.path,guide.shape[:2],self.check_active))
            prepared=scopes.enter_context(prepare_transform(person,stage.transform,self.artifacts.scratch,guide=guide,
                inverse_field=field,tile=512,max_work_pixels=4194304,check_active=self.check_active))
            transformed=scopes.enter_context(read_array(prepared.array,guide.shape[:2],'float32',self.check_active,unit_interval=True))
            yield transformed;self.check_active()

    @contextmanager
    def after_color(self,rgb,alpha,*,private_sequence=None):
        with ExitStack() as scopes:
            current=GeometryPixels(rgb,alpha,self.source.canvas,self.source.transform_chain)
            if self.crop:
                stage,params,source,target=self.crop[0]
                policy=next(entry[4] for entry in self.verified if entry[0].task_id==stage.task_id)
                if stage.operation=='image.outpaint':
                    from .outpaint import prepare_region_outpaint
                    current=scopes.enter_context(prepare_region_outpaint(self.artifacts,params,source,target,current.rgb,current.alpha,
                        pool=self.generation_pool,backend=self.backend,profile=self.profile,verify_runtime=self.verify_runtime,check_active=self.check_active,
                        private_input=None if private_sequence is None else private_sequence.input('image.outpaint',current.rgb,current.alpha)))
                else:current=scopes.enter_context(prepare_region_crop(stage.source,source,params.plan,policy['plan'],current.rgb,current.alpha,self.artifacts.scratch,check_active=self.check_active))
                if private_sequence is not None:private_sequence.record(stage.operation,current.rgb,current.alpha)
            if self.upscale:
                stage,params,source,target=self.upscale[0]
                current=scopes.enter_context(prepare_region_upscale(params,target,current.rgb,current.alpha,self.upscale_provider,self.artifacts.scratch,
                    backend=self.backend,model_lock_sha=self.model_lock_sha,check_active=self.check_active))
                if private_sequence is not None:private_sequence.record('image.upscale',current.rgb,current.alpha)
            final=self.verified[-1][3]
            # Metadata follows actual authenticated transforms even when later
            # same-canvas stages changed pixels but appended no transform.
            yield GeometryPixels(current.rgb,current.alpha,final.canvas,final.transform_chain)
            self.check_active()


def verify_region_luts(verified,artifacts,*,check_active=lambda:None):
    """Authenticate each exact external LUT occurrence without mutable keys."""
    from photo_worker.runtime.arrays import read_lut
    checked={}
    for _,_,_,_,policy in verified:
        resolved=policy.get('resolved');lut=resolved.absolute_lut if resolved is not None else None
        if lut is None:continue
        key=(lut.artifact.artifact_id,lut.artifact.sha256)
        # A repeated binary ref cannot silently acquire a different grid shape.
        if key in checked:
            if checked[key]!=lut.size:raise ValueError('REGION_LUT_SHAPE_BINDING_INVALID')
            check_active();continue
        artifacts.require_declared_input(lut.artifact)
        with artifacts.input(lut.artifact,suffix='.npy') as local:
            with read_lut(local.path,lut.size,check_active):check_active()
        checked[key]=lut.size


def require_skipped_body_identity(field,*,check_active=lambda:None):
    """Exact skipped-field proof with bounded panorama work and lease checks."""
    h,w=field.shape[:2]
    for row in range(0,h,256):
        for column in range(0,w,256):
            check_active()
            tile=field[row:row+256,column:column+256]
            yy,xx=np.mgrid[row:row+tile.shape[0],column:column+tile.shape[1]]
            if not np.array_equal(tile[...,0],xx) or not np.array_equal(tile[...,1],yy):
                raise ValueError('REGION_BODY_SKIPPED_FIELD_NOT_IDENTITY')
    check_active()


def require_person_mask_identity(person,original_mask,*,check_active=lambda:None):
    """Compare authenticated person masks without full-width temporaries."""
    check_active()
    if person.shape!=original_mask.shape:
        raise ValueError('REGION_PERSON_MODEL_REPLAY_MISMATCH')
    h,w=person.shape[:2]
    for row in range(0,h,256):
        for column in range(0,w,256):
            check_active()
            if not np.array_equal(person[row:row+256,column:column+256],
                                  original_mask[row:row+256,column:column+256]):
                raise ValueError('REGION_PERSON_MODEL_REPLAY_MISMATCH')
    check_active()
