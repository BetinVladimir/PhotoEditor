"""Concrete native Region color path; independently approved installation only.

This is deliberately a bounded provider, not a replacement for missing native
face/mask/depth/model evidence. Required module coverage remains exact.
"""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass, field
from pathlib import Path
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.learning.color_render import render_materialized_style
from photo_worker.runtime.executor import OperationFailure
from .adapter import RenderedRegionCanvas
from .calibration import Qualification, PinnedRegionCalibrationCatalog
from .mapped import prepare_region_full_canvas
from .renderer import PinnedRegionRenderer


@dataclass(frozen=True)
class NativeRegionColorProvider:
    qualification: Qualification
    icc_root: Path
    verify_qualification: object
    max_input_bytes: int
    person_pool: object = None
    portrait_pool: object = None
    denoise_provider: object = None
    upscale_pool: object = None
    inpaint_pool: object = None
    generation_pool: object = None
    masking_pool: object = None
    _masking_factories: tuple = field(init=False,repr=False)
    _generation_factory: object = field(init=False,repr=False)
    _inpaint_factory: object = field(init=False,repr=False)
    _upscale_factories: tuple = field(init=False, repr=False)
    _qualification_sha: str = field(init=False, repr=False)
    _person_factory: object = field(init=False, repr=False)
    _portrait_factories: tuple = field(init=False, repr=False)
    _denoise_pins: tuple = field(init=False, repr=False)
    renderer_version = 'v2.2'

    @property
    def supported_modules(self):
        modules={'color','strength','restoration','geometry','composition','portrait_detail','background','region_adjustments','series','defects','culling'}
        if self.person_pool is not None:modules.update(('adaptive','local'))
        if self.portrait_pool is not None:modules.add('beauty')
        if self.inpaint_pool is not None:modules.add('inpainting')
        if self.generation_pool is not None:modules.add('generation')
        if self.masking_pool is not None:modules.add('masking')
        return frozenset(modules)

    def __post_init__(self):
        if (not isinstance(self.qualification, Qualification)
                or not isinstance(self.icc_root, Path) or not self.icc_root.is_absolute()
                or not callable(self.verify_qualification)
                or type(self.max_input_bytes) is not int or not 1 <= self.max_input_bytes <= 2**53-1):
            raise ValueError('REGION_NATIVE_INSTALLATION_INVALID')
        object.__setattr__(self,'qualification',self.qualification.model_copy(deep=True))
        object.__setattr__(self,'_qualification_sha',canonical_hash(self.qualification))
        object.__setattr__(self,'_person_factory',None)
        object.__setattr__(self,'_portrait_factories',())
        object.__setattr__(self,'_denoise_pins',())
        object.__setattr__(self,'_upscale_factories',())
        object.__setattr__(self,'_inpaint_factory',None)
        object.__setattr__(self,'_masking_factories',())
        if self.masking_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            pool=self.masking_pool;models=('segformer_b0_ade','bisenet_face19','mobilesam')
            if not isinstance(pool,ModelPool) or pool.lock.lock_id!=self.model_lock_sha:raise ValueError('REGION_MASKING_INSTALLATION_INVALID')
            entries={entry.id:entry for entry in pool.lock.models}
            for model in models:
                entry=entries.get(model)
                if (entry is None or model not in pool.factories or not entry.approved_for_install
                        or self.backend not in entry.backends or entry.backends[self.backend].status!='passed'):
                    raise ValueError('REGION_MASKING_INSTALLATION_INVALID:'+model)
                verify_model(pool.root/self.model_lock_sha/model,entry)
            object.__setattr__(self,'_masking_factories',tuple((model,pool.factories[model]) for model in models))
        object.__setattr__(self,'_generation_factory',None)
        if self.generation_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            pool=self.generation_pool
            if not isinstance(pool,ModelPool) or pool.lock.lock_id!=self.model_lock_sha:raise ValueError('REGION_GENERATION_INSTALLATION_INVALID')
            entries=[entry for entry in pool.lock.models if entry.id=='sd15_inpaint']
            if (len(entries)!=1 or 'sd15_inpaint' not in pool.factories or not entries[0].approved_for_install
                    or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                raise ValueError('REGION_GENERATION_INSTALLATION_INVALID')
            verify_model(pool.root/self.model_lock_sha/'sd15_inpaint',entries[0])
            object.__setattr__(self,'_generation_factory',pool.factories['sd15_inpaint'])
        if self.inpaint_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            pool=self.inpaint_pool
            if not isinstance(pool,ModelPool) or pool.lock.lock_id!=self.model_lock_sha:raise ValueError('REGION_INPAINT_INSTALLATION_INVALID')
            entries=[entry for entry in pool.lock.models if entry.id=='big_lama']
            if (len(entries)!=1 or 'big_lama' not in pool.factories or not entries[0].approved_for_install
                    or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                raise ValueError('REGION_INPAINT_INSTALLATION_INVALID')
            verify_model(pool.root/self.model_lock_sha/'big_lama',entries[0])
            object.__setattr__(self,'_inpaint_factory',pool.factories['big_lama'])
        if self.upscale_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            if not isinstance(self.upscale_pool,ModelPool) or self.upscale_pool.lock.lock_id!=self.model_lock_sha:
                raise ValueError('REGION_UPSCALE_INSTALLATION_INVALID')
            factories=[]
            for model in ('realesrgan_x2','realesrgan_x4'):
                entries=[entry for entry in self.upscale_pool.lock.models if entry.id==model]
                if (len(entries)!=1 or model not in self.upscale_pool.factories or not entries[0].approved_for_install
                        or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                    raise ValueError('REGION_UPSCALE_INSTALLATION_INVALID')
                verify_model(self.upscale_pool.root/self.model_lock_sha/model,entries[0])
                factories.append((model,self.upscale_pool.factories[model]))
            object.__setattr__(self,'_upscale_factories',tuple(factories))
        if self.person_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            if not isinstance(self.person_pool,ModelPool) or self.person_pool.lock.lock_id!=self.model_lock_sha:
                raise ValueError('REGION_PERSON_INSTALLATION_INVALID')
            model='lraspp_mobilenet_v3_large'
            entries=[entry for entry in self.person_pool.lock.models if entry.id==model]
            if (len(entries)!=1 or model not in self.person_pool.factories or not entries[0].approved_for_install
                    or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                raise ValueError('REGION_PERSON_INSTALLATION_INVALID')
            verify_model(self.person_pool.root/self.model_lock_sha/model,entries[0])
            object.__setattr__(self,'_person_factory',self.person_pool.factories[model])
        if self.portrait_pool is not None:
            from photo_worker.models.pool import ModelPool
            from photo_worker.models.install import verify_model
            if not isinstance(self.portrait_pool,ModelPool) or self.portrait_pool.lock.lock_id!=self.model_lock_sha:
                raise ValueError('REGION_PORTRAIT_INSTALLATION_INVALID')
            factories=[]
            for model in ('sfd','fan2d','bisenet_face19'):
                entries=[entry for entry in self.portrait_pool.lock.models if entry.id==model]
                if (len(entries)!=1 or model not in self.portrait_pool.factories or not entries[0].approved_for_install
                        or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                    raise ValueError('REGION_PORTRAIT_INSTALLATION_INVALID')
                verify_model(self.portrait_pool.root/self.model_lock_sha/model,entries[0])
                factories.append((model,self.portrait_pool.factories[model]))
            object.__setattr__(self,'_portrait_factories',tuple(factories))
        if self.denoise_provider is not None:
            from photo_worker.v2.render_native_base import NativeDenoiseProvider
            from photo_worker.models.install import verify_model
            from photo_worker.models.pool import ModelPool
            provider=self.denoise_provider
            if (type(provider) is not NativeDenoiseProvider or provider.backend!=self.backend
                    or not isinstance(provider.pool,ModelPool) or provider.pool.lock.lock_id!=self.model_lock_sha):
                raise ValueError('REGION_DENOISE_INSTALLATION_INVALID')
            model='scunet_color_real_psnr'
            entries=[entry for entry in provider.pool.lock.models if entry.id==model]
            if (len(entries)!=1 or model not in provider.pool.factories or not entries[0].approved_for_install
                    or self.backend not in entries[0].backends or entries[0].backends[self.backend].status!='passed'):
                raise ValueError('REGION_DENOISE_INSTALLATION_INVALID')
            verify_model(provider.pool.root/self.model_lock_sha/model,entries[0])
            object.__setattr__(self,'_denoise_pins',(provider.pool,provider.backend,provider.verify_runtime,
                tuple(sorted(provider.peak().items())),provider.pool.factories[model]))

    @property
    def backend(self): return self.qualification.backend
    @property
    def runtime_sha256(self): return self.qualification.runtime_lock_sha
    @property
    def model_lock_sha(self): return self.qualification.model_lock_sha
    @property
    def icc_lock_sha(self): return self.qualification.icc_lock_sha
    @property
    def device_profile_sha(self): return self.qualification.device_profile_sha

    def verify_runtime(self):
        # No truthy/None callback result can invent an approved installation.
        if canonical_hash(self.qualification)!=self._qualification_sha:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        supplied=self.qualification.model_copy(deep=True)
        if self.verify_qualification(supplied) is not True:
            raise OperationFailure('REGION_RUNTIME_UNAVAILABLE')
        if canonical_hash(supplied)!=self._qualification_sha or canonical_hash(self.qualification)!=self._qualification_sha:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.person_pool is not None and (self.person_pool.lock.lock_id!=self.model_lock_sha
                or self.person_pool.factories.get('lraspp_mobilenet_v3_large') is not self._person_factory):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.masking_pool is not None and (self.masking_pool.lock.lock_id!=self.model_lock_sha
                or any(self.masking_pool.factories.get(model) is not factory for model,factory in self._masking_factories)):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.portrait_pool is not None and (self.portrait_pool.lock.lock_id!=self.model_lock_sha
                or any(self.portrait_pool.factories.get(model) is not factory for model,factory in self._portrait_factories)):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.upscale_pool is not None and (self.upscale_pool.lock.lock_id!=self.model_lock_sha
                or any(self.upscale_pool.factories.get(model) is not factory for model,factory in self._upscale_factories)):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.inpaint_pool is not None and (self.inpaint_pool.lock.lock_id!=self.model_lock_sha
                or self.inpaint_pool.factories.get('big_lama') is not self._inpaint_factory):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.generation_pool is not None and (self.generation_pool.lock.lock_id!=self.model_lock_sha
                or self.generation_pool.factories.get('sd15_inpaint') is not self._generation_factory):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if self.denoise_provider is not None:
            provider=self.denoise_provider
            current=(provider.pool,provider.backend,provider.verify_runtime,
                tuple(sorted(provider.peak().items())),provider.pool.factories.get('scunet_color_real_psnr'))
            if current!=self._denoise_pins or provider.pool.lock.lock_id!=self.model_lock_sha:
                raise OperationFailure('RUNTIME_LOCK_MISMATCH')

    def admit_resources(self, ctx, params):
        ctx.check_active()
        if ctx.lease.execution_profile.get('backend') != self.backend:
            raise OperationFailure('CONFIG_INVALID')
        self.verify_runtime()
        ctx.check_active()

    @contextmanager
    def render(self, ctx, artifacts, params, source, style):
        from photo_worker.v2.render_modules import required_render_modules
        if not required_render_modules(style) <= self.supported_modules:
            raise OperationFailure('REGION_RUNTIME_UNAVAILABLE')
        restoration=style.extensions.restoration
        from .collection_selection import validate_collection_selection
        validate_collection_selection(style,params.execution)
        if style.extensions.masking is not None and params.execution is None:
            raise OperationFailure('REGION_EXECUTION_REQUIRED')
        if restoration is not None and (restoration.upscale!='off' and self.upscale_pool is None
                or restoration.denoise!='off' and self.denoise_provider is None):
            raise OperationFailure('REGION_RUNTIME_UNAVAILABLE')
        if style.defects.enabled and style.defects.policy=='conservative_auto' and self.portrait_pool is None:
            raise OperationFailure('REGION_FACE_ANALYSIS_UNAVAILABLE')
        self.verify_runtime();ctx.check_active()
        geometry=None
        if params.execution is not None:
            from .geometry_replay import RegionGeometryReplay
            from types import SimpleNamespace
            upscale_provider=None if self.upscale_pool is None else SimpleNamespace(pool=self.upscale_pool,backend=self.backend,verify_runtime=self.verify_runtime)
            geometry=RegionGeometryReplay(artifacts,params,source,style,backend=self.backend,model_lock_sha=self.model_lock_sha,
                upscale_provider=upscale_provider,generation_pool=self.generation_pool,profile=ctx.lease.execution_profile,
                verify_runtime=self.verify_runtime,check_active=ctx.check_active)
        elif (style.geometry.enabled or style.extensions.composition is not None and style.extensions.composition.enabled
                or restoration is not None and restoration.upscale!='off'
                or style.extensions.portrait_detail is not None and style.extensions.portrait_detail.enabled
                or style.extensions.background is not None and style.extensions.background.mode!='keep'
                or bool(style.extensions.region_adjustments)
                or style.extensions.series is not None and style.extensions.series.enabled):
            raise OperationFailure('REGION_EXECUTION_REQUIRED')
        if geometry is None and (style.defects.enabled or restoration is not None and restoration.sensor_dust!='off'):
            raise OperationFailure('REGION_EXECUTION_REQUIRED')
        if geometry is None and style.extensions.inpainting is not None and style.extensions.inpainting.enabled:
            raise OperationFailure('REGION_EXECUTION_REQUIRED')
        if geometry is None and style.extensions.generation is not None and style.extensions.generation.enabled:
            raise OperationFailure('REGION_EXECUTION_REQUIRED')
        @contextmanager
        def pipeline(metadata, effective, original):
            # Existing full-canvas color math, strength and ICC semantics.
            # No model is acquired on a different backend or silently skipped.
            with ExitStack() as contexts:
                sequence=None
                if geometry is not None:
                    from .private_frames import RegionPrivateSequence
                    command=ctx.lease.command
                    sequence=RegionPrivateSequence(owner=(command.task_id,command.attempt,ctx.lease.fence),
                        recipe=params.frozen_recipe,source=metadata,rgb=original.rgb,alpha=original.alpha,
                        execution=params.execution,verified=geometry.verified,check_active=ctx.check_active,
                        base_style=geometry.base_style,effective_style=effective)
                def record(operation,rgb,alpha,phase=None):
                    if sequence is not None:sequence.record(operation,rgb,alpha,phase=phase)
                def private_input(operation,rgb,alpha,phase=None):
                    return None if sequence is None else sequence.input(operation,rgb,alpha,phase=phase)
                faces=()
                color_style=effective
                if effective.beauty.enabled:
                    from photo_worker.contracts.style import Disabled,StyleV2
                    data=effective.model_dump(mode='python',by_alias=True)
                    data['beauty']=Disabled().model_dump(mode='python')
                    color_style=StyleV2.model_validate(data)
                if geometry is not None:
                    from photo_worker.contracts.style import Disabled,StyleV2
                    data=color_style.model_dump(mode='python',by_alias=True)
                    data['geometry']=Disabled().model_dump(mode='python')
                    data['defects']=Disabled().model_dump(mode='python')
                    data['extensions']['composition']=None
                    data['extensions']['portrait_detail']=None
                    data['extensions']['background']=None
                    data['extensions']['inpainting']=None
                    data['extensions']['generation']=None
                    data['extensions']['culling']=None
                    color_style=StyleV2.model_validate(data)
                stats=None
                if effective.mode=='adaptive' or effective.local.enabled:
                    from photo_worker.operations.learning.person_stats import open_holdout_person_stats
                    stats=contexts.enter_context(open_holdout_person_stats(original.rgb,original.alpha,
                        self.person_pool,self.backend,self.model_lock_sha,artifacts.scratch,ctx.check_active))
                from .denoise import prepare_region_denoise
                denoised=contexts.enter_context(prepare_region_denoise(artifacts,params.source.ref,original.rgb,
                    effective,self.denoise_provider,backend=self.backend,model_lock_sha=self.model_lock_sha,
                    verify_runtime=self.verify_runtime,check_active=ctx.check_active))
                record('image.denoise',denoised,original.alpha)
                working_rgb=denoised;working_alpha=original.alpha;working_metadata=metadata
                if geometry is not None:
                    before=contexts.enter_context(geometry.before_color(denoised,original.alpha))
                    working_rgb=before.rgb;working_alpha=before.alpha
                    record('geometry.body',working_rgb,working_alpha)
                    working_metadata=metadata.model_copy(update=dict(canvas=before.canvas,transform_chain=before.transform_chain))
                if stats is not None and geometry is not None:
                    from dataclasses import replace
                    transformed_person=contexts.enter_context(geometry.person_mask(stats.mask,working_rgb))
                    stats=replace(stats,mask=transformed_person)
                if color_style.extensions.restoration is not None:
                    data=color_style.model_dump(mode='python',by_alias=True)
                    data['extensions']['restoration']=None
                    from photo_worker.contracts.style import StyleV2
                    color_style=StyleV2.model_validate(data)
                if color_style.extensions.background is not None and color_style.extensions.background.mode=='keep':
                    data=color_style.model_dump(mode='python',by_alias=True);data['extensions']['background']=None
                    from photo_worker.contracts.style import StyleV2
                    color_style=StyleV2.model_validate(data)
                owner=contexts.enter_context(artifacts.scratch.reserve(original.rgb.size*4+8192))
                path=owner.path('region-native-color','.npy')
                target=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=original.rgb.shape)
                try:
                    from .color_inputs import prepare_region_color_inputs
                    color_inputs=contexts.enter_context(prepare_region_color_inputs(artifacts,effective,
                        () if geometry is None else geometry.verified,working_metadata,
                        None if geometry is None else geometry.document,check_active=ctx.check_active))
                    color_arguments=dict(
                        person_reliable=False if stats is None else stats.reliable,
                        features=None if stats is None else stats.features,
                        coverage_counts=None if stats is None else stats.coverage_counts,
                        person_mask=None if stats is None else stats.mask,
                        region_masks=color_inputs.region_masks,series_correction=color_inputs.series_correction,
                        series_item_id=color_inputs.series_item_id,check_active=ctx.check_active)
                    dust=restoration is not None and restoration.sensor_dust!='off'
                    if geometry is not None:
                        from photo_worker.algorithms.style import resolve_style
                        from photo_worker.operations.learning.color_render import render_resolved_color
                        from .conservative import prepare_region_conservative
                        resolved_color=resolve_style(color_style,features=color_arguments['features'],
                            coverage_counts=color_arguments['coverage_counts'],person_reliable=color_arguments['person_reliable'],strict_ood=False)
                        render_arguments={key:value for key,value in color_arguments.items()
                            if key not in ('features','coverage_counts','person_reliable')}
                        if not resolved_color.local.enabled:render_arguments['person_mask']=None
                        global_owner=contexts.enter_context(artifacts.scratch.reserve(working_rgb.size*4+8192))
                        global_path=global_owner.path('region-global-before-dust','.npy')
                        global_pixels=np.lib.format.open_memmap(global_path,mode='w+',dtype='<f4',shape=working_rgb.shape)
                        contexts.callback(global_pixels._mmap.close)
                        render_resolved_color(working_rgb,global_pixels,resolved_color,style=color_style,phase='global',**render_arguments)
                        global_pixels.flush();global_owner.validate_file(global_path);global_pixels.flags.writeable=False
                        record('image.color',global_pixels,working_alpha)
                        dust_pixels=global_pixels
                        if dust:
                            dust_pixels=contexts.enter_context(prepare_region_conservative(artifacts,effective,geometry.verified,
                                'sensor.dust_auto_repair',global_pixels,working_alpha,profile=ctx.lease.execution_profile,
                                verify_runtime=self.verify_runtime,check_active=ctx.check_active,
                                private_input=private_input('sensor.dust_auto_repair',global_pixels,working_alpha)))
                            record('sensor.dust_auto_repair',dust_pixels,working_alpha)
                            from .reviewed import prepare_region_reviewed
                            dust_pixels=contexts.enter_context(prepare_region_reviewed(artifacts,effective,geometry.verified,
                                'sensor.dust_repair',dust_pixels,working_alpha,profile=ctx.lease.execution_profile,
                                verify_runtime=self.verify_runtime,check_active=ctx.check_active,private_sequence=sequence))
                            record('sensor.dust_repair',dust_pixels,working_alpha)
                        render_resolved_color(working_rgb,target,resolved_color,style=color_style,
                            phase='local',global_output=dust_pixels,**render_arguments)
                    else:
                        render_materialized_style(working_rgb,target,color_style,**color_arguments)
                    target.flush();owner.validate_file(path);target.flags.writeable=False
                    record('image.local',target,working_alpha)
                    if effective.beauty.enabled:
                        from .portrait import prepare_region_faces
                        from photo_worker.operations.portrait.prepare import prepare_beauty
                        from photo_worker.algorithms.effective import effective_beauty_params
                        from photo_worker.runtime.arrays import read_array
                        # Match Apply's color/local → fresh face analysis → beauty
                        # ordering. These are private fresh geometry observations,
                        # not documents claiming original RGB reference identity.
                        faces=contexts.enter_context(prepare_region_faces(target,working_alpha,working_metadata,
                            self.portrait_pool,self.backend,self.model_lock_sha,artifacts.scratch,
                            verify_runtime=self.verify_runtime,check_active=ctx.check_active))
                        beauty_config=effective_beauty_params(effective.beauty,effective.strength)
                        beauty=contexts.enter_context(prepare_beauty(target,working_metadata,faces,beauty_config,artifacts.scratch,
                            tile=512,max_roi_pixels=4194304,check_active=ctx.check_active))
                        pixels=contexts.enter_context(read_array(beauty.image,target.shape,'float32',ctx.check_active,unit_interval=True))
                    else:pixels=target
                    record('portrait.beauty',pixels,working_alpha)
                    if geometry is not None:
                        from .detail import prepare_region_detail
                        pixels=contexts.enter_context(prepare_region_detail(artifacts,effective,geometry.verified,pixels,
                            working_alpha,working_metadata,check_active=ctx.check_active))
                        record('portrait.detail',pixels,working_alpha)
                        from .conservative import prepare_region_conservative
                        fresh_defect_faces=()
                        if effective.defects.enabled and effective.defects.policy=='conservative_auto':
                            from .portrait import prepare_region_faces
                            fresh_defect_faces=contexts.enter_context(prepare_region_faces(pixels,working_alpha,working_metadata,
                                self.portrait_pool,self.backend,self.model_lock_sha,artifacts.scratch,
                                verify_runtime=self.verify_runtime,check_active=ctx.check_active))
                        pixels=contexts.enter_context(prepare_region_conservative(artifacts,effective,geometry.verified,
                            'defects.auto_repair',pixels,working_alpha,profile=ctx.lease.execution_profile,
                            verify_runtime=self.verify_runtime,check_active=ctx.check_active,
                            private_input=private_input('defects.auto_repair',pixels,working_alpha),fresh_faces=fresh_defect_faces))
                        record('defects.auto_repair',pixels,working_alpha)
                        from .reviewed import prepare_region_reviewed
                        pixels=contexts.enter_context(prepare_region_reviewed(artifacts,effective,geometry.verified,
                            'defects.repair',pixels,working_alpha,profile=ctx.lease.execution_profile,
                            verify_runtime=self.verify_runtime,check_active=ctx.check_active,private_sequence=sequence))
                        record('defects.repair',pixels,working_alpha)
                        from .background import prepare_region_background
                        background=contexts.enter_context(prepare_region_background(artifacts,effective,geometry.verified,pixels,
                            working_alpha,working_metadata,check_active=ctx.check_active))
                        record('background.compose',background.rgb,background.alpha)
                        record('background.blur',background.rgb,background.alpha)
                        from .generation import prepare_region_generation
                        generation_options=dict(pool=self.generation_pool,backend=self.backend,model_lock_sha=self.model_lock_sha,
                            profile=ctx.lease.execution_profile,verify_runtime=self.verify_runtime,check_active=ctx.check_active)
                        pixels=contexts.enter_context(prepare_region_generation(artifacts,effective,geometry.verified,'background',background.rgb,background.alpha,private_input=private_input('image.generate',background.rgb,background.alpha,'background'),**generation_options))
                        record('image.generate',pixels,background.alpha,'background')
                        from .inpaint import prepare_region_inpaint
                        pixels=contexts.enter_context(prepare_region_inpaint(artifacts,effective,geometry.verified,pixels,background.alpha,
                            pool=self.inpaint_pool,backend=self.backend,model_lock_sha=self.model_lock_sha,
                            profile=ctx.lease.execution_profile,verify_runtime=self.verify_runtime,check_active=ctx.check_active,
                            private_input=private_input('image.inpaint',pixels,background.alpha)))
                        record('image.inpaint',pixels,background.alpha)
                        pixels=contexts.enter_context(prepare_region_generation(artifacts,effective,geometry.verified,'generate',pixels,background.alpha,private_input=private_input('image.generate',pixels,background.alpha,'generate'),**generation_options))
                        record('image.generate',pixels,background.alpha,'generate')
                        with geometry.after_color(pixels,background.alpha,private_sequence=sequence) as rendered:
                            sequence.finish()
                            yield RenderedRegionCanvas(rendered.rgb,rendered.alpha,rendered.canvas,rendered.transform_chain)
                    else:yield RenderedRegionCanvas(pixels,working_alpha)
                    ctx.check_active()
                finally:
                    target._mmap.close()
        with prepare_region_full_canvas(artifacts,source,style,pipeline,
                max_input_bytes=self.max_input_bytes,check_active=ctx.check_active) as full:
            yield full
            ctx.check_active();self.verify_runtime()


def install_native_region_color(*, qualification, icc_root, verify_qualification,
                                runtime_path, calibration_catalog, max_input_bytes, person_pool=None,portrait_pool=None,denoise_provider=None,upscale_pool=None,inpaint_pool=None,generation_pool=None,masking_pool=None):
    """Trusted assembly consumed by build_worker(region_renderer=...).

    Backend/device/workload calibration is mandatory on CUDA and Metal. No
    environment-selected imports, default evidence or automatic activation.
    """
    if type(calibration_catalog) is not PinnedRegionCalibrationCatalog:
        raise ValueError('REGION_CALIBRATION_UNAVAILABLE')
    provider=NativeRegionColorProvider(qualification,icc_root,verify_qualification,max_input_bytes,person_pool,portrait_pool,denoise_provider,upscale_pool,inpaint_pool,generation_pool,masking_pool)
    return PinnedRegionRenderer(provider,runtime_path,qualification.runtime_lock_sha,
        calibration_catalog=calibration_catalog)
