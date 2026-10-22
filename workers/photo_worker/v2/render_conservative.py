"""Qualified LayerSet dispatch reuses the automatic task engine exactly."""
from contextlib import contextmanager
from copy import deepcopy
import re
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.conservative import DefectAutoRepairParams,DustAutoRepairParams
from photo_worker.operations.conservative import prepare_conservative_repair
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels,RenderLayerProposal
from .render_layer_params import RenderLayerOperator
from .render_providers import RenderLayerProvider

def conservative_render_provider_peak(width, height):
    """Conservative native repair overhead, not a measured qualification.

    Keep the repair engine's minimum RAM admission and conservatively reserve
    the same bound for its mapped intermediates. Shared render buffers and final
    publication output are added separately by render_resources.
    """
    from photo_worker.operations.sensor.prepare_repair import dust_repair_ram_bytes
    bound = max(1 << 30, dust_repair_ram_bytes(width, height))
    return dict(ram_bytes=bound, scratch_bytes=bound, vram_bytes=0, output_bytes=0)


class PinnedConservativeProviderFactory:
    def __init__(self,*,runtime_sha256,verify_runtime,validate_binding):
        if not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None or not all(callable(value) for value in (verify_runtime,validate_binding)):
            raise ValueError('RENDER_CONSERVATIVE_INSTALLATION_INVALID')
        self.runtime_sha256=runtime_sha256;self.verify_runtime=verify_runtime;self.validate_binding=validate_binding

    def __call__(self,artifacts,inputs,profile,check_active):
        if not isinstance(profile,dict) or not callable(check_active):raise ValueError('RENDER_CONSERVATIVE_INSTALLATION_INVALID')
        owned_profile=deepcopy(profile)
        def provider(operation,model):
            def bind(params,state):
                check_active()
                if not isinstance(params,model):raise ValueError('RENDER_CONSERVATIVE_PARAMS_INVALID')
                self.validate_binding(deepcopy(params),deepcopy(state));check_active()
            @contextmanager
            def prepare(item,current):
                if item.layer.operation!=operation or item.layer.operation_version!='1' or item.runtime_sha256!=self.runtime_sha256 or not isinstance(item.params,model):raise ValueError('RENDER_CONSERVATIVE_BINDING_INVALID')
                params=model.model_validate_json(canonical_bytes(artifacts.contract(item.layer.params,model)))
                if canonical_bytes(params)!=canonical_bytes(item.params):raise ValueError('RENDER_CONSERVATIVE_DOCUMENT_CHANGED')
                from .render_resources import admit_render
                image=artifacts.contract(params.image,WorkingImage)
                peak=conservative_render_provider_peak(image.canvas.width,image.canvas.height)
                admit_render(owned_profile,image.canvas.width,image.canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak)
                with prepare_conservative_repair(artifacts,params,execution_profile=owned_profile,current=current,verify_runtime=self.verify_runtime,check_active=check_active) as stage:
                    @contextmanager
                    def pixels():
                        if stage.path is None:yield current
                        else:
                            with read_array(stage.path,(stage.image.canvas.height,stage.image.canvas.width,3),'float32',check_active,unit_interval=True) as rgb:
                                yield AdjustmentPixels(rgb,current.alpha,current.generated)
                    with pixels() as result:
                        from .render_effects import RenderLayerEffect,effect_review_required
                        effect=RenderLayerEffect(item.layer.id,operation,'1',self.runtime_sha256,(),False,conservative_report=deepcopy(stage.report))
                        effect_review_required(effect)
                        yield RenderLayerProposal(result,effect);check_active()
            return RenderLayerProvider(RenderLayerOperator(operation,'1',self.runtime_sha256,model,self.verify_runtime,bind),prepare)
        return (provider('defects.auto_repair',DefectAutoRepairParams),provider('sensor.dust_auto_repair',DustAutoRepairParams))
