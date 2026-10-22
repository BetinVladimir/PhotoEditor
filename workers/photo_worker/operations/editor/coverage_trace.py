"""Compute binary AI support from a SQL-authenticated canonical image trace."""
from contextlib import ExitStack, contextmanager
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage, MaskRef, DenseWarpTransform
from photo_worker.contracts.restoration import GenerationReport, InpaintReport
from photo_worker.algorithms.generated_support import generated_support_tiles
from photo_worker.runtime.arrays import read_array, read_inverse_field


@contextmanager
def prepare_coverage_trace(artifacts, params, check_active):
    trace = params.coverage_trace
    if not trace: raise ValueError('EDITOR_COVERAGE_TRACE_REQUIRED')
    def owned(ref, model):
        check_active(); value=model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)));check_active();return value
    current_ref = trace[0].source
    current = owned(current_ref,WorkingImage)
    seen=set(); requested=params.generated is not None
    with ExitStack() as owners:
        def allocate(image):
            lifetime=ExitStack();owners.callback(lifetime.close)
            owner=lifetime.enter_context(artifacts.scratch.reserve(4*image.canvas.height*image.canvas.width+8192))
            path=owner.path('editor-coverage','.npy')
            array=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(image.canvas.height,image.canvas.width))
            lifetime.callback(array._mmap.close)
            return lifetime,owner,path,array
        lifetime,owner,path,coverage=allocate(current)
        for y in range(0,coverage.shape[0],256):check_active();coverage[y:y+256]=0
        def union(mask,image,target):
            if (mask.selector!='generated' or mask.protected or mask.canvas!=image.canvas
                    or mask.transform_chain!=image.transform_chain):raise ValueError('EDITOR_COVERAGE_MASK_BINDING')
            with artifacts.input(mask.artifact,suffix='.npy') as local:
                with read_array(local.path,target.shape,'float32',check_active,unit_interval=True) as physical:
                    for y in range(0,target.shape[0],256):
                        check_active();part=physical[y:y+256]
                        if not np.all((part==0)|(part==1)):raise ValueError('EDITOR_COVERAGE_MASK_BINARY')
                        target[y:y+256]=np.maximum(target[y:y+256],part)
        if params.generated is not None:union(params.generated,current,coverage)
        for entry in trace:
            check_active()
            if entry.task_id in seen or entry.source!=current_ref:raise ValueError('EDITOR_COVERAGE_TRACE_CHAIN')
            seen.add(entry.task_id); target=owned(entry.output,WorkingImage)
            if target.color!=current.color or target.canvas.source_sha256!=current.canvas.source_sha256:
                raise ValueError('EDITOR_COVERAGE_TRACE_ORIGIN')
            source_steps=current.transform_chain.steps;target_steps=target.transform_chain.steps
            if target.canvas==current.canvas and target.transform_chain==current.transform_chain:
                # Pointwise color preserves support. Other image effects may use
                # arbitrary learned/context support, so conservatively carry any
                # existing AI influence to the complete output canvas.
                if entry.operation not in ('image.color','image.local','image.generate','image.inpaint'):
                    present=False
                    for y in range(0,coverage.shape[0],256):check_active();present|=bool(np.any(coverage[y:y+256]))
                    if present:
                        for y in range(0,coverage.shape[0],256):check_active();coverage[y:y+256]=1
            else:
                if len(target_steps)!=len(source_steps)+1 or target_steps[:-1]!=source_steps:
                    raise ValueError('EDITOR_COVERAGE_TRACE_TRANSFORM')
                step=target_steps[-1];next_lifetime,next_owner,next_path,next_coverage=allocate(target)
                with ExitStack() as field_owner:
                    field=None
                    if isinstance(step,DenseWarpTransform):
                        local=field_owner.enter_context(artifacts.input(step.inverse_field,suffix='.npy'))
                        field=field_owner.enter_context(read_inverse_field(local.path,(target.canvas.height,target.canvas.width),check_active))
                    for x,y,values in generated_support_tiles(coverage,step,operation=entry.operation,inverse_field=field,check_active=check_active):
                        next_coverage[y:y+values.shape[0],x:x+values.shape[1]]=values
                # Release prior scratch/mapping immediately; only two canvases
                # are live at once, rather than one file for every trace node.
                lifetime.close()
                lifetime=next_lifetime;coverage=next_coverage;owner=next_owner;path=next_path
            if entry.operation in ('image.generate','image.outpaint','image.inpaint'):
                if entry.report is None:raise ValueError('EDITOR_COVERAGE_REPORT_REQUIRED')
                report=owned(entry.report,InpaintReport if entry.operation=='image.inpaint' else GenerationReport)
                if report.image!=entry.source or report.generated_mask is None:raise ValueError('EDITOR_COVERAGE_REPORT_BINDING')
                mask=owned(report.generated_mask,MaskRef);union(mask,target,coverage);requested=True
            if entry.operation=='editor.layers_precompose':
                from photo_worker.operations.optical.contracts import ImageResult,ImageReport
                if entry.report is None:raise ValueError('EDITOR_COVERAGE_PRECOMPOSE_RESULT_REQUIRED')
                result=owned(entry.report,ImageResult);report=owned(result.report,ImageReport)
                if (result.working_image!=entry.output or report.operation!='editor.layers_precompose'
                        or report.status not in ('pass','review') or report.outside_mask_max_error!=0
                        or len(report.sources)!=1 or report.sources[0].source!=entry.source
                        or target.canvas!=current.canvas or target.transform_chain!=current.transform_chain):
                    raise ValueError('EDITOR_COVERAGE_PRECOMPOSE_BINDING')
                if result.generated_mask is not None:
                    union(owned(result.generated_mask,MaskRef),target,coverage);requested=True
            current_ref=entry.output;current=target
        if current_ref!=params.processed_source.source or requested!=params.generative_requested:
            raise ValueError('EDITOR_COVERAGE_TRACE_TARGET')
        coverage.flush();owner.validate_file(path);check_active()
        yield path,owner,current
        check_active()
