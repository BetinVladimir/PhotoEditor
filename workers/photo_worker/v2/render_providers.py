"""One inventory binds qualified Params loading to actual layer execution."""
from contextlib import contextmanager
from copy import deepcopy
from dataclasses import dataclass
from typing import Callable
from .render_layer_params import RenderLayerOperator,LoadedRenderLayer,_bounded_identifier


@dataclass(frozen=True)
class RenderLayerProvider:
    operator: RenderLayerOperator
    prepare: Callable


class RenderLayerProviders:
    """Task-local explicit inventory; no fallback or automatic qualification."""
    def __init__(self,providers:tuple[RenderLayerProvider,...],*,check_active=lambda:None):
        import re
        if not isinstance(providers,tuple) or len(providers)>128 or not callable(check_active):
            raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
        registry={}
        for provider in providers:
            if not isinstance(provider,RenderLayerProvider) or not isinstance(provider.operator,RenderLayerOperator):
                raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
            operator=provider.operator
            if not _bounded_identifier(operator.operation,80) or not _bounded_identifier(operator.version,20):
                raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
            key=(operator.operation,operator.version)
            from photo_worker.contracts.base import Contract
            if (key in registry
                    or not isinstance(operator.runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',operator.runtime_sha256) is None
                    or not isinstance(operator.params_model,type) or not issubclass(operator.params_model,Contract)
                    or not callable(operator.verify_runtime) or not callable(operator.validate_binding)
                    or not callable(provider.prepare)):
                raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
            registry[key]=provider
        self._registry=registry
        self.operators=tuple(provider.operator for provider in providers)
        self.check_active=check_active

    @contextmanager
    def __call__(self,item,current):
        self.check_active()
        if not isinstance(item,LoadedRenderLayer):raise ValueError('RENDER_PROVIDER_LAYER_INVALID')
        owned=deepcopy(item)
        provider=self._registry.get((owned.layer.operation,owned.layer.operation_version))
        if provider is None:raise ValueError('RENDER_OPERATOR_UNAVAILABLE')
        operator=provider.operator
        if owned.runtime_sha256!=operator.runtime_sha256 or not isinstance(owned.params,operator.params_model):
            raise ValueError('RENDER_PROVIDER_BINDING_CHANGED')
        operator.verify_runtime();self.check_active()
        # Loaded Params were source-bound by load_render_layer_params. Execution
        # rechecks runtime immediately before entering the provider context.
        with provider.prepare(deepcopy(owned),current) as proposal:
            self.check_active()
            yield proposal
            self.check_active()


class RenderProviderAssembly:
    """Shared task-local installation for worker execution and adoption replay.

    Each factory receives (artifacts, owned_inputs, owned_profile, check_active).
    It installs concrete providers; the renderer still proves qualification and
    admits resources. Replay intentionally does not install result measurement.
    """
    def __init__(self,*,base_factory,layer_factory,override_factory,measure_factory,preparation):
        factories=(base_factory,layer_factory,override_factory,measure_factory)
        required={'supported_modules','max_params_document_bytes','max_override_document_bytes',
                  'max_input_bytes','max_mask_bytes','max_tile_bytes'}
        if (not all(callable(factory) for factory in factories)
                or not isinstance(preparation,dict) or set(preparation)!=required
                or not isinstance(preparation['supported_modules'],frozenset)
                or any(not isinstance(name,str) or not name for name in preparation['supported_modules'])
                or any(type(preparation[name]) is not int or not 1<=preparation[name]<=2**53-1
                       for name in required-{'supported_modules'})):
            raise ValueError('RENDER_PROVIDER_ASSEMBLY_INVALID')
        self._factories=factories
        self._preparation=deepcopy(preparation)

    def _load(self,artifacts,inputs,profile,check_active,*,measurement):
        if not isinstance(profile,dict) or not callable(check_active):
            raise ValueError('RENDER_PROVIDER_ASSEMBLY_INVALID')
        check_active()
        owned_inputs,owned_profile=deepcopy(inputs),deepcopy(profile)
        def build(factory):
            value=factory(artifacts,deepcopy(owned_inputs),deepcopy(owned_profile),check_active)
            check_active()
            return value
        base=build(self._factories[0])
        if not callable(base):raise ValueError('RENDER_BASE_PROVIDER_INVALID')
        layers=RenderLayerProviders(build(self._factories[1]),check_active=check_active)
        overrides=build(self._factories[2])
        result=dict(operators=layers.operators,render_proposal=layers,render_base=base,
                    scalar_overrides=overrides,preparation=deepcopy(self._preparation))
        if measurement:
            measure=build(self._factories[3])
            if not callable(measure):raise ValueError('RENDER_MEASUREMENT_PROVIDER_INVALID')
            result['measure']=measure
        return result

    def worker(self,ctx,artifacts,inputs):
        return self._load(artifacts,inputs,ctx.lease.execution_profile,ctx.check_active,measurement=True)

    def replay(self,artifacts,inputs,scoped,*,check_active=None):
        if check_active is None:
            check_active=getattr(artifacts,'check_active',None)
        return self._load(artifacts,inputs,scoped.execution_profile(),check_active,measurement=False)


def combine_render_layer_factories(*factories):
    """Merge explicit native factories; duplicate operations fail closed.

    Returned callback fits RenderProviderAssembly.layer_factory. Each component
    receives detached input/profile metadata, preventing cross-factory rewrites.
    """
    if len(factories)>128 or any(not callable(factory) for factory in factories):
        raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
    def install(artifacts,inputs,profile,check_active):
        if not callable(check_active):raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
        owned_inputs,owned_profile=deepcopy(inputs),deepcopy(profile)
        providers=[]
        for factory in factories:
            check_active()
            installed=factory(artifacts,deepcopy(owned_inputs),deepcopy(owned_profile),check_active)
            check_active()
            if not isinstance(installed,tuple) or len(providers)+len(installed)>128:
                raise ValueError('RENDER_PROVIDER_INVENTORY_INVALID')
            providers.extend(installed)
        # Validate before returning any partially installed dispatch inventory.
        RenderLayerProviders(tuple(providers),check_active=check_active)
        return tuple(providers)
    return install
