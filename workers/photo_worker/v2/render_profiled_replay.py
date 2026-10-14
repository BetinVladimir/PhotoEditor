"""Installed-runtime replay through the existing complete authored preparation."""
from contextlib import contextmanager
import re
from .adoption import AdoptionInvalid
from .render_adoption_inputs import RenderAdoptionInputs,resolve_render_source_parent
from .render_qualification_pin import RenderQualificationPin
from .render_replay_reader import RenderReplayReader
from .render_replay_artifacts import RenderReplayArtifacts
from .render_runtime import load_render_runtime_lock
from .render_resources import validate_render_execution_profile
from .render_inputs import load_frozen_render_inputs
from .render_authored import prepare_authored_render


class ProfiledRenderReplay:
    """Operator supplies approved providers and an owned scratch context factory.

    No submitted result, pixels or metric report is accepted as reconstruction
    input. Providers must implement the complete authored stage inventory and
    independently verify model/device qualification. This class verifies build
    identity/profile matching and uses unchanged preparation/resource admission.
    """
    def __init__(self, *, backend, runtime_path, runtime_sha256, model_lock_sha,
                 icc_lock_sha, provider_peak, verify_qualification,
                 load_providers, make_scratch, qualification_pin, check_active=lambda: None):
        if (backend not in ('cpu','cuda','mps') or any(not isinstance(value,str)
                or re.fullmatch('[0-9a-f]{64}',value) is None
                for value in (runtime_sha256,model_lock_sha,icc_lock_sha))
                or not isinstance(provider_peak,dict)
                or set(provider_peak)!={'ram_bytes','scratch_bytes','vram_bytes','output_bytes'}
                or any(type(value) is not int or not 0<=value<=2**53-1 for value in provider_peak.values())
                or not all(callable(value) for value in (verify_qualification,load_providers,make_scratch,check_active))):
            raise ValueError('Invalid profiled Render replay configuration')
        if (not isinstance(qualification_pin,RenderQualificationPin)
                or (qualification_pin.backend,qualification_pin.runtime_lock_sha,qualification_pin.model_lock_sha)
                != (backend,runtime_sha256,model_lock_sha)):
            raise ValueError('Replay qualification identity does not match configured runtime')
        self.qualification_pin=qualification_pin
        self.backend=backend;self.runtime_path=runtime_path
        self.runtime_sha256=runtime_sha256;self.model_lock_sha=model_lock_sha;self.icc_lock_sha=icc_lock_sha
        self._peak=tuple(provider_peak.items())
        self.verify_qualification=verify_qualification;self.load_providers=load_providers
        self.make_scratch=make_scratch;self.check_active=check_active

    def verify_runtime(self):
        self.check_active()
        load_render_runtime_lock(self.runtime_path,self.runtime_sha256)
        self.check_active();self.verify_qualification(self.qualification_pin);self.check_active()

    @contextmanager
    def __call__(self, reader, scoped, frozen):
        if not isinstance(reader,RenderReplayReader) or not isinstance(scoped,RenderAdoptionInputs):
            raise AdoptionInvalid('RENDER_ADOPTION_REPLAY_REQUIRED')
        if scoped.qualification_pin!=self.qualification_pin:
            raise AdoptionInvalid('RENDER_REPLAY_QUALIFICATION_MISMATCH')
        profile=scoped.execution_profile()
        if (profile.get('model_lock_sha')!=self.model_lock_sha
                or profile.get('icc_lock_sha')!=self.icc_lock_sha):
            raise AdoptionInvalid('RENDER_REPLAY_PROFILE_INVALID')
        validate_render_execution_profile(profile,frozen,backend=self.backend,
            runtime_sha=self.runtime_sha256,tile_size=profile.get('tile_size'),alpha_mode=profile.get('alpha_mode'))
        self.verify_runtime()
        with self.make_scratch(scoped) as scratch:
            artifacts=RenderReplayArtifacts(reader,scratch,self.check_active)
            inputs=load_frozen_render_inputs(artifacts,scoped.recipe,check_active=self.check_active)
            if inputs.recipe!=frozen:
                raise AdoptionInvalid('RENDER_REPLAY_RECIPE_CHANGED')
            source_parent=resolve_render_source_parent(artifacts,scoped,frozen.recipe.source.source,
                check_active=self.check_active)
            supplied=self.load_providers(artifacts,inputs,scoped)
            required={'operators','scalar_overrides','render_base','render_proposal','preparation'}
            if (not isinstance(supplied,dict) or set(supplied)!=required
                    or not isinstance(supplied['preparation'],dict)):
                raise AdoptionInvalid('RENDER_REPLAY_PROVIDERS_INVALID')
            from .render_providers import RenderLayerProviders
            if (isinstance(supplied['render_proposal'],RenderLayerProviders)
                    and supplied['operators']!=supplied['render_proposal'].operators):
                raise AdoptionInvalid('RENDER_REPLAY_PROVIDERS_INVALID')
            options=dict(supplied['preparation'])
            reserved={'execution_profile','provider_peak','tile_size','alpha_mode','check_active','verify_base_runtime','source_result'}
            if reserved.intersection(options):
                raise AdoptionInvalid('RENDER_REPLAY_PROVIDERS_INVALID')
            self.check_active()
            with prepare_authored_render(artifacts,inputs,supplied['operators'],supplied['scalar_overrides'],
                    supplied['render_base'],supplied['render_proposal'],self.verify_runtime,
                    execution_profile=profile,provider_peak=dict(self._peak),tile_size=profile['tile_size'],
                    alpha_mode=profile['alpha_mode'],source_result=source_parent,
                    check_active=self.check_active,**options) as prepared:
                self.check_active()
                yield prepared.render.composition.pixels
                self.check_active()
