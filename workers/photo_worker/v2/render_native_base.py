"""Native portrait/background provider for the authored Render callback boundary."""
from contextlib import contextmanager
from dataclasses import dataclass
from copy import deepcopy
from .render_modules import required_render_modules
from .render_portrait import prepare_portrait_base
from .render_portrait_background import (PORTRAIT_BACKGROUND_MODULES,
    prepare_portrait_background_base,prepare_frozen_asset_background_base)
from .render_background import render_frozen_compose_params,render_blur_params


@dataclass(frozen=True)
class NativeBaseEvidence:
    """Verified analysis owners supplied by a scoped context loader.

    The loader must prove source/model/ICC identity and retain face/mask/depth
    arrays until its context exits. This structure is not qualification proof.
    """
    faces: tuple
    person_reliable: bool
    subject_ref: object = None
    subject: object = None
    depth_ref: object = None
    depth: object = None
    permanent: object = None
    features: object = None
    coverage_counts: object = None
    person_mask: object = None
    region_masks: object = None
    denoise_params_ref: object = None


class NativePortraitBackgroundBase:
    """Task-local render_base(inputs, style, original, masks) implementation.

    QualifiedTaskRenderer still verifies runtime/qualification and performs
    complete resource admission before invoking this provider. Unsupported
    authored modules cannot be silently omitted; full geometry/generative/etc
    pipelines must use their dedicated provider assembly.
    """
    supported_modules=PORTRAIT_BACKGROUND_MODULES

    def __init__(self,artifacts,*,execution_profile,retained_resources,registry,load_evidence,
                 tile_size=512,max_roi_pixels=4194304,denoise_provider=None,check_active=lambda:None):
        if (not callable(load_evidence) or type(tile_size) is not int or tile_size not in (256,512,1024)
                or type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304):
            raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
        self.denoise_provider=denoise_provider
        if denoise_provider is not None:
            if not isinstance(denoise_provider,NativeDenoiseProvider):
                raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
            self.supported_modules=self.supported_modules|{'restoration'}
        self.artifacts=artifacts
        self.execution_profile=deepcopy(execution_profile)
        self.retained_resources=deepcopy(retained_resources)
        self.registry=registry;self.load_evidence=load_evidence
        self.tile_size=tile_size;self.max_roi_pixels=max_roi_pixels;self.check_active=check_active

    @contextmanager
    def __call__(self,inputs,style,original,masks):
        self.check_active()
        inputs=deepcopy(inputs);style=deepcopy(style)
        background=style.effective.extensions.background
        mode=None if background is None else background.mode
        restoration=style.effective.extensions.restoration
        if restoration is not None and (self.denoise_provider is None or mode is not None
                or restoration.denoise=='off' or restoration.upscale!='off' or restoration.sensor_dust!='off'):
            raise ValueError('RENDER_COMPLETE_PROVIDER_REQUIRED')
        if (not required_render_modules(style.effective)<=self.supported_modules
                or mode not in (None,'keep','blur','solid','transparent','asset')):
            raise ValueError('RENDER_COMPLETE_PROVIDER_REQUIRED')
        with self.load_evidence(deepcopy(inputs),deepcopy(style),original,masks) as evidence:
            self.check_active()
            if (not isinstance(evidence,NativeBaseEvidence) or not isinstance(evidence.faces,tuple)
                    or type(evidence.person_reliable) is not bool):
                raise ValueError('RENDER_NATIVE_BASE_EVIDENCE_INVALID')
            options=dict(faces=evidence.faces,person_reliable=evidence.person_reliable,
                execution_profile=deepcopy(self.execution_profile),permanent=evidence.permanent,
                features=evidence.features,coverage_counts=evidence.coverage_counts,
                person_mask=evidence.person_mask,region_masks=evidence.region_masks,
                tile_size=self.tile_size,max_roi_pixels=self.max_roi_pixels,check_active=self.check_active)
            if restoration is not None:
                from .render_portrait import prepare_denoised_portrait_base
                provider=self.denoise_provider
                stage=prepare_denoised_portrait_base(self.artifacts,inputs,style,original,masks,
                    params_ref=evidence.denoise_params_ref,pool=provider.pool,backend=provider.backend,
                    provider_peak=provider.peak(),verify_runtime=provider.verify_runtime,**options)
            elif mode is None:
                stage=prepare_portrait_base(self.artifacts,inputs,style,original,masks,**options)
            elif mode=='asset':
                stage=prepare_frozen_asset_background_base(self.artifacts,inputs,style,original,masks,
                    registry=self.registry,subject_ref=evidence.subject_ref,subject=evidence.subject,
                    retained_resources=deepcopy(self.retained_resources),**options)
            else:
                params={}
                if mode=='blur':
                    params=dict(blur_params=render_blur_params(style,inputs.recipe.recipe.source.source,
                        evidence.subject_ref,evidence.depth_ref,tile_size=self.tile_size),
                        subject=evidence.subject,depth=evidence.depth)
                elif mode in ('solid','transparent'):
                    params=dict(compose_params=render_frozen_compose_params(style,inputs.recipe,
                        evidence.subject_ref,tile_size=self.tile_size),subject=evidence.subject)
                stage=prepare_portrait_background_base(self.artifacts,inputs,style,original,masks,**params,**options)
            with stage as result:
                yield result
                self.check_active()


@contextmanager
def load_pinned_background_arrays(artifacts,inputs,subject_document,depth_document,*,
        execution_profile,retained_resources,check_active=lambda:None):
    """Load declared same-canvas subject/depth evidence with bounded ownership.

    TaskArtifacts enforces closure membership. Caller proves adopted analysis
    provenance, model qualification and reference selection; matching geometry
    alone does not establish that authority. Returned metadata is owned and
    pixel mappings remain alive until exit. None depth is allowed for compose.
    """
    from contextlib import ExitStack
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.contracts.images import MaskRef,RelativeDepth
    from photo_worker.runtime.arrays import read_array
    from .render_background_decode import _admit_decode
    check_active()
    if (not isinstance(subject_document,ArtifactRef)
            or depth_document is not None and not isinstance(depth_document,ArtifactRef)):
        raise ValueError('RENDER_BACKGROUND_ANALYSIS_REFS_INVALID')
    profile=deepcopy(execution_profile);retained=deepcopy(retained_resources)
    h,w=inputs.source.canvas.height,inputs.source.canvas.width
    if any(type(v) is not int or v<1 for v in (h,w)) or h*w>200_000_000:
        raise ValueError('RENDER_BACKGROUND_SOURCE_INVALID')
    count=1+(depth_document is not None)
    required=dict(ram_bytes=count*(4*h*w+8192)+64*1024*1024,
                  scratch_bytes=count*(4*h*w+8192))
    _admit_decode(profile,retained,required)
    subject=MaskRef.model_validate_json(canonical_bytes(artifacts.contract(subject_document,MaskRef)))
    if (subject.protected or subject.selector not in ('person','subject') or subject.confidence<.75
            or subject.canvas!=inputs.source.canvas or subject.transform_chain!=inputs.source.transform_chain):
        raise ValueError('RENDER_BACKGROUND_SUBJECT_INVALID')
    depth=None
    if depth_document is not None:
        depth=RelativeDepth.model_validate_json(canonical_bytes(artifacts.contract(depth_document,RelativeDepth)))
        if (depth.invalid_mask is not None or depth.canvas!=inputs.source.canvas
                or depth.transform_chain!=inputs.source.transform_chain):
            raise ValueError('RENDER_BACKGROUND_DEPTH_INVALID')
    with ExitStack() as stack:
        def mapped(reference):
            check_active()
            local=stack.enter_context(artifacts.input(reference,suffix='.npy'))
            return stack.enter_context(read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
        alpha=mapped(subject.artifact)
        distances=None if depth is None else mapped(depth.artifact)
        check_active()
        yield subject,alpha,depth,distances
        check_active()


def discover_pinned_background_refs(artifacts,source,*,require_depth,execution_profile,
                                    retained_resources,check_active=lambda:None):
    """Select one exact same-canvas typed analysis ref from verified closure.

    No preference between person/subject or two complete depths is invented.
    Analysis provenance/qualification still requires independent loader proof.
    Missing closure metadata is not permission to generate/borrow other masks.
    """
    from photo_worker.contracts.images import MaskRef,RelativeDepth
    from photo_worker.contracts.base import canonical_bytes
    from .render_background_decode import _admit_decode
    if type(require_depth) is not bool:
        raise ValueError('RENDER_BACKGROUND_DISCOVERY_INVALID')
    check_active()
    _admit_decode(deepcopy(execution_profile),deepcopy(retained_resources),
        dict(ram_bytes=64*1024*1024,scratch_bytes=0))
    subjects=[];depths=[]
    for reference in artifacts.declared_contract_refs('MaskRef',maximum=512):
        check_active()
        mask=MaskRef.model_validate_json(canonical_bytes(artifacts.contract(reference,MaskRef)))
        if (mask.selector in ('person','subject') and not mask.protected
                and mask.canvas==source.canvas and mask.transform_chain==source.transform_chain
                and mask.confidence>=.75):subjects.append(reference)
    if len(subjects)!=1:
        raise ValueError('RENDER_BACKGROUND_SUBJECT_SELECTION_INVALID')
    if require_depth:
        for reference in artifacts.declared_contract_refs('RelativeDepth',maximum=512):
            check_active()
            depth=RelativeDepth.model_validate_json(canonical_bytes(artifacts.contract(reference,RelativeDepth)))
            if (depth.invalid_mask is None and depth.canvas==source.canvas
                    and depth.transform_chain==source.transform_chain):depths.append(reference)
        if len(depths)!=1:
            raise ValueError('RENDER_BACKGROUND_DEPTH_SELECTION_INVALID')
    check_active()
    return subjects[0],None if not require_depth else depths[0]


class NativePortraitBackgroundFactory:
    """Install the native base in RenderProviderAssembly for each task/replay.

    Trusted installer supplies the scoped evidence-loader factory and retained
    resource calculator. Neither constitutes qualification evidence. Factories
    must only construct dependencies; array IO remains inside the base context.
    """
    def __init__(self,registry,*,evidence_factory,retained_resources,max_roi_pixels=4194304,denoise_provider=None):
        if (not callable(evidence_factory) or not callable(retained_resources)
                or type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304):
            raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
        self.registry=registry
        self.denoise_provider=denoise_provider
        self.evidence_factory=evidence_factory
        self.retained_resources=retained_resources
        self.max_roi_pixels=max_roi_pixels

    def __call__(self,artifacts,inputs,profile,check_active):
        from .render_background_decode import _admit_decode
        if (not isinstance(profile,dict) or not callable(check_active)
                or type(profile.get('tile_size')) is not int
                or profile['tile_size'] not in (256,512,1024)):
            raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
        if self.denoise_provider is not None and (not isinstance(self.denoise_provider,NativeDenoiseProvider)
                or profile.get('backend')!=self.denoise_provider.backend):
            raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
        check_active()
        owned_profile=deepcopy(profile)
        retained=deepcopy(self.retained_resources(deepcopy(inputs),deepcopy(owned_profile)))
        check_active()
        # Refuse underfunded retained owners before constructing evidence IO.
        _admit_decode(owned_profile,retained,dict(ram_bytes=0,scratch_bytes=0))
        loader=self.evidence_factory(artifacts,deepcopy(inputs),deepcopy(owned_profile),check_active)
        check_active()
        if not callable(loader):raise ValueError('RENDER_NATIVE_BASE_EVIDENCE_INVALID')
        return NativePortraitBackgroundBase(artifacts,execution_profile=owned_profile,
            retained_resources=retained,registry=self.registry,load_evidence=loader,
            tile_size=owned_profile['tile_size'],max_roi_pixels=self.max_roi_pixels,denoise_provider=self.denoise_provider,
            check_active=check_active)


@dataclass(frozen=True,init=False)
class NativeDenoiseProvider:
    """Installer-owned model dependency; no source authority or qualification inference."""
    pool: object
    backend: str
    verify_runtime: object
    _peak: tuple

    def __init__(self,pool,*,backend,provider_peak,verify_runtime):
        from .render_resources import render_resources
        if backend not in ('cpu','cuda','mps') or not callable(verify_runtime):
            raise ValueError('RENDER_NATIVE_BASE_CONFIG_INVALID')
        render_resources(1,1,mask_refs=0,mask_nodes=0,provider_peak=provider_peak)
        object.__setattr__(self,'pool',pool)
        object.__setattr__(self,'backend',backend)
        object.__setattr__(self,'verify_runtime',verify_runtime)
        object.__setattr__(self,'_peak',tuple(sorted(deepcopy(provider_peak).items())))

    def peak(self):return dict(self._peak)
