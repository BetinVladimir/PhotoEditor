"""Independent installed native Region execution; submitted pixels are no oracle."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.renderer import PinnedRegionRenderer
from photo_worker.operations.region.result import RegionDerivativeManifest
from photo_worker.operations.region.presentation import prepare_region_presentation,write_region_presentation
from photo_worker.operations.learning.recipe import materialize_frozen_style
from photo_worker.runtime.scratch import ScratchSpace
from .adoption import AdoptionInvalid
from .lens_bypass import _ref
from .render_replay_reader import RenderReplayReader


@dataclass(frozen=True)
class _File:
    path: Path


class RegionReplayArtifacts:
    """Exact declared inputs only; no publication or staged output interface."""
    def __init__(self,reader,scratch,check_active):
        self.reader=RenderReplayReader(reader,check_active);self.scratch=scratch;self.check=check_active

    def require_declared_input(self,ref):self.reader.evidence(_ref(ref))

    def contract(self,ref,model):
        raw=self.reader.read_json(_ref(ref),1048576);value=model.model_validate_json(raw)
        if canonical_bytes(value)!=raw:raise AdoptionInvalid('REGION_REPLAY_CANONICAL_REQUIRED')
        return value

    @contextmanager
    def stream(self,ref):
        with BytesIO(self.reader.read_json(_ref(ref),1048576)) as stream:yield stream

    @contextmanager
    def input(self,ref,*,suffix='.npy'):
        evidence=self.reader.evidence(_ref(ref))
        expected={'.json':{'application/json'},'.npy':{'application/x-npy','application/octet-stream'},
            '.jpg':{'image/jpeg'},'.jpeg':{'image/jpeg'},'.png':{'image/png'},'.tif':{'image/tiff'},'.tiff':{'image/tiff'}}
        if suffix not in expected or evidence.media_type not in expected[suffix]:
            raise AdoptionInvalid('REGION_REPLAY_INPUT_MEDIA_INVALID')
        with self.scratch.reserve(evidence.bytes) as reservation:
            path=reservation.path('region-replay-input',suffix)
            blocks=iter(self.reader.iter_bytes(_ref(ref),evidence.bytes))
            try:
                with self.scratch.open_writer(path,reservation) as stream:
                    for block in blocks:self.check();stream.write(block);reservation.validate()
                reservation.validate_file(path);self.check();yield _File(path);self.check()
            finally:
                close=getattr(blocks,'close',None)
                if close is not None:close()


@dataclass(frozen=True)
class NativeRegionReplay:
    renderer: PinnedRegionRenderer
    scratch_root: Path
    max_bytes: int
    device_budget: object = None

    def __post_init__(self):
        if (type(self.renderer) is not PinnedRegionRenderer or not isinstance(self.scratch_root,Path)
                or not self.scratch_root.is_absolute() or self.scratch_root.is_symlink()
                or not self.scratch_root.is_dir() or type(self.max_bytes) is not int or not 1<=self.max_bytes<=2**53-1):
            raise ValueError('REGION_REPLAY_INSTALLATION_INVALID')
        self.renderer.verify_runtime()

    def verify(self,reader,params_ref,root_ref,*,task_id,attempt,fence,profile,check_active):
        input_reader=RenderReplayReader(reader,check_active)
        params_raw=input_reader.read_json(params_ref,1048576);params=RegionPreviewParams.model_validate_json(params_raw)
        if canonical_bytes(params)!=params_raw:raise AdoptionInvalid('REGION_REPLAY_CANONICAL_REQUIRED')
        source_body=input_reader.read_json(_ref(params.source.ref),1048576);source=params.validate_source(source_body)
        base_raw=input_reader.read_json(_ref(params.frozen_recipe.base_style),1048576);base=StyleV2.model_validate_json(base_raw)
        if canonical_bytes(base)!=base_raw:raise AdoptionInvalid('REGION_REPLAY_CANONICAL_REQUIRED')
        effective=materialize_frozen_style(params.frozen_recipe,base,params.frozen_recipe.base_style,
            params.frozen_recipe.source,params.frozen_recipe.icc_lock_sha,params.frozen_recipe.model_lock_sha)
        raw=reader.read_json(root_ref,1048576);manifest=RegionDerivativeManifest.model_validate_json(raw)
        if canonical_bytes(manifest)!=raw or sha256(raw).hexdigest()!=root_ref.sha256:
            raise AdoptionInvalid('REGION_REPLAY_MANIFEST_INVALID')
        estimate=profile.get('resource_estimate',{}) if isinstance(profile,dict) else {}
        bound=estimate.get('scratch_bytes')
        if type(bound) is not int or not 1<=bound<=self.max_bytes:
            raise AdoptionInvalid('REGION_REPLAY_SCRATCH_EXCEEDED')
        from copy import deepcopy
        owned_profile=deepcopy(profile)
        def active():
            check_active()
            if profile!=owned_profile:raise AdoptionInvalid('REGION_REPLAY_PROFILE_CHANGED')
        ctx=SimpleNamespace(check_active=active,lease=SimpleNamespace(command=SimpleNamespace(task_id=task_id,attempt=attempt),
            fence=fence,execution_profile=owned_profile))
        if self.device_budget is not None:
            from .region_adopter_config import RegionAdopterBudget
            if (type(self.device_budget) is not RegionAdopterBudget
                    or self.device_budget.backend!=self.renderer.backend):
                raise AdoptionInvalid('REGION_REPLAY_DEVICE_BUDGET_INVALID')
            needed=estimate.get('vram_bytes')
            if type(needed) is not int or needed<0 or needed>self.device_budget.remaining():
                raise AdoptionInvalid('REGION_REPLAY_DEVICE_CAPACITY_UNAVAILABLE')
        self.renderer.admit_resources(ctx,params)
        with ScratchSpace(self.scratch_root,uuid4(),task_id,attempt,bound) as scratch:
            artifacts=RegionReplayArtifacts(reader,scratch,active)
            with self.renderer.render(ctx,artifacts,params,source,effective) as result:
                from contextlib import ExitStack
                with ExitStack() as scopes:
                    def inverse(ref,shape):
                        from photo_worker.runtime.arrays import read_inverse_field
                        local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
                        return scopes.enter_context(read_inverse_field(local.path,shape,active))
                    presentation=prepare_region_presentation(params,source_body,result.rgb,result.alpha,self.renderer.icc_root,
                        runtime_icc_lock_sha=self.renderer.icc_lock_sha,target_canvas=result.target_canvas,
                        target_chain=result.target_transform_chain,load_inverse_field=inverse,check_active=active)
                    if presentation.projection!=manifest.projection or presentation.cache_key!=manifest.cache_key:
                        raise AdoptionInvalid('REGION_REPLAY_PROJECTION_MISMATCH')
                    with scratch.reserve(24*1024*1024) as reservation:
                        path=reservation.path('region-replay-derivative','.webp' if presentation.alpha is not None else '.jpg')
                        count=write_region_presentation(path,presentation,check_active=active)
                        reservation.validate_file(path);digest=sha256()
                        with path.open('rb') as body:
                            while True:
                                active();block=body.read(65536)
                                if not block:break
                                digest.update(block)
                        if count!=manifest.bytes or digest.hexdigest()!=manifest.derivative.sha256:
                            raise AdoptionInvalid('REGION_REPLAY_PIXELS_MISMATCH')
        active();self.renderer.verify_runtime();active()
