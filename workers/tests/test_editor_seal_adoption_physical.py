"""Independent seal replay uses real NPY input/output bytes, never worker flags."""
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.quality import QualityReport,TechnicalMetrics
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.editor_effects import EditorEffectsSealParams,EditorExecutedBaseReceipt
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef as EvidenceRef,AdoptionInvalid
from photo_worker.v2.editor_seal_adoption import EditorSealAdoptionInputs,verify_editor_effects_seal
from test_interchange_import_stage import adopted_fixture
from test_interchange_export_style import style_fixture
from test_render_executed_base import sealed_fixture


class Reader:
    def __init__(self):self.items={};self.reads=[]
    def add(self,value,source='input',role=None):
        binary=isinstance(value,np.ndarray)
        if binary:
            stream=BytesIO();np.save(stream,value,allow_pickle=False);raw=stream.getvalue()
        else:raw=canonical_bytes(value)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        self.items[ref.artifact_id]=(raw,source,role,'application/octet-stream' if binary else 'application/json')
        return ref
    def evidence(self,ref):
        raw,source,role,media=self.items[ref.artifact_id]
        return ArtifactEvidence(EvidenceRef(ref.artifact_id,ref.sha256),media,len(raw),role,source)
    def read_json(self,ref,limit):
        raw=self.items[ref.artifact_id][0];assert len(raw)<=limit;self.reads.append(ref.artifact_id);return raw
    def iter_bytes(self,ref,limit):
        raw=self.items[ref.artifact_id][0];assert len(raw)<=limit
        for offset in range(0,len(raw),4096):yield raw[offset:offset+4096]


def physical_fixture(*,generated=False,clipped=False,quality_review=False,nonzero_change=False,trace=False,tamper_generated=False):
    reader=Reader();_,_,_,raw=adopted_fixture();image=WorkingImage.model_validate_json(raw)
    shape=(image.canvas.height,image.canvas.width)
    rgb=np.full((*shape,3),.4,np.float32)
    if clipped:rgb[0,0,0]=1.2
    image=image.model_copy(update={'pixels':reader.add(rgb)})
    source=reader.add(image)
    _,_,_,docs,prior,_=sealed_fixture()
    binding=prior.processed_source.model_copy(update={'source':source,'canvas_hash':canonical_hash(image.canvas)})
    style=reader.add(StyleV2.model_validate(style_fixture()))
    origin=docs[prior.original_recipe.artifact_id]
    recipe=origin.recipe.model_copy(update={'source':binding,'base_style':style})
    origin=origin.model_copy(update={'recipe':recipe,'recipe_sha256':canonical_hash(recipe),
        'scalars':origin.scalars.model_copy(update={'source':binding,'base_style':style})})
    origin_ref=reader.add(origin)
    quality=reader.add(QualityReport(target=source,target_kind='working',stage='master',threshold_profile_id='master_v1',
        status='review' if quality_review else 'pass',technical=TechnicalMetrics(below_zero_fraction=0,above_one_fraction=0,
        clipped_black_fraction=0,clipped_white_fraction=0,sharpness_laplacian=1,seam_p99_delta=0,finite=True),reference=None,reasons=[]))
    mask=None
    if generated:
        support=np.zeros(shape,np.float32);support[1,2]=1
        mask=MaskRef(artifact=reader.add(support),canvas=image.canvas,transform_chain=image.transform_chain,
            object_id=None,selector='generated',confidence=1,model_lock_id=None,protected=False)
    trace_entries=[]
    if trace:
        from photo_worker.contracts.editor_effects import EditorCoverageStep
        from photo_worker.contracts.restoration import GenerationReport
        original_binding=binding
        output=reader.add(image)
        binding=binding.model_copy(update={'source':output})
        trace_report=reader.add(GenerationReport(image=source,mask=mask.artifact,model_id='sd15_inpaint',model_lock_id='c'*64,
            backend='cpu',seed=1,guidance_scale=7.5,generated_pixels=1,protected_pixels=0,side=None,
            source_rect=[0,0,shape[1],shape[0]],generated_mask=reader.add(mask)))
        trace_entries=[EditorCoverageStep(task_id=uuid4(),result_id=uuid4(),attempt=1,fence=2,
            operation='image.generate',source=source,output=output,report=trace_report)]
        source=output
        quality=reader.add(QualityReport(target=source,target_kind='working',stage='master',threshold_profile_id='master_v1',
            status='pass',technical=TechnicalMetrics(below_zero_fraction=0,above_one_fraction=0,clipped_black_fraction=0,
            clipped_white_fraction=0,sharpness_laplacian=1,seam_p99_delta=0,finite=True),reference=None,reasons=[]))
    else:original_binding=binding
    params=EditorEffectsSealParams(schema_version=1,build_run_id=uuid4(),original_recipe=origin_ref,original_source=original_binding,
        processed_source=binding,quality=quality,executed_style=style,compilation_sha='a'*64,trace_sha=canonical_hash([entry.model_dump(mode='json') for entry in trace_entries]) if trace else 'b'*64,
        generative_requested=generated,generated=None if trace else mask,inherited_review=False,coverage_trace=trace_entries)
    params_ref=reader.add(params)
    review=generated or clipped or quality_review
    report=ImageReport(schema_version=1,status='review' if review else 'pass',operation='editor.effects_seal',skipped_count=0,
        reason='EDITOR_EXECUTED_BASE_REVIEW' if review else None,outside_mask_max_error=0,
        clipping_fraction=1/(shape[0]*shape[1]) if clipped else 0,identity=None,transform=None,sources=[binding],registrations=[])
    change_pixels=np.zeros(shape,np.float32)
    if nonzero_change:change_pixels[0,0]=1
    changed=MaskRef(artifact=reader.add(change_pixels,'output','changed_pixels'),canvas=image.canvas,
        transform_chain=image.transform_chain,object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=False)
    published_mask=mask
    if trace:
        physical=np.zeros(shape,np.float32)
        if not tamper_generated:physical[1,2]=1
        published_mask=mask.model_copy(update={'artifact':reader.add(physical,'output','generated_pixels')})
    result=ImageResult(schema_version=1,working_image=source,report=reader.add(report,'output','image_report'),
        changed_mask=reader.add(changed,'output','changed_mask'),generated_mask=reader.add(published_mask,'output','generated_mask') if published_mask else None)
    receipt=EditorExecutedBaseReceipt(schema_version=1,build_run_id=params.build_run_id,original_recipe=origin_ref,
        original_source=original_binding,processed_source=binding,executed_style=style,compilation_sha=params.compilation_sha,
        trace_sha=params.trace_sha,quality=quality,image_result=reader.add(result,'output','image_result'),
        generative_requested=generated,inherited_review=False)
    root=reader.add(receipt,'output','executed_base_receipt')
    profile={'backend':'cpu','resource_estimate':{'pixel_count':shape[0]*shape[1],'ram_bytes':70<<20,
        'scratch_bytes':70<<20,'output_bytes':7<<20,'vram_bytes':0}}
    return reader,EvidenceRef(root.artifact_id,root.sha256),EditorSealAdoptionInputs(EvidenceRef(params_ref.artifact_id,params_ref.sha256),canonical_bytes(profile),params.build_run_id),changed,result,report


@pytest.mark.parametrize('options',[{}, {'generated':True},{'clipped':True},{'quality_review':True},{'generated':True,'trace':True}])
def test_physical_identity_seal_preserves_actual_metrics_and_inherited_generation(tmp_path,options):
    reader,root,scope,*_=physical_fixture(**options)
    verify_editor_effects_seal(reader,root,scope,scratch_root=tmp_path,max_bytes=80<<20,check_active=lambda:None)


def test_worker_nonzero_change_is_refused_even_with_correct_rehashed_object(tmp_path):
    reader,root,scope,*_=physical_fixture(nonzero_change=True)
    # Every submitted JSON/NPY object has its real immutable hash. Independent
    # reconstruction must still reject a truthful nonzero changed-pixel output.
    with pytest.raises(AdoptionInvalid,match='NOT_IDENTITY'):
        verify_editor_effects_seal(reader,root,scope,scratch_root=tmp_path,max_bytes=80<<20,check_active=lambda:None)


def test_trace_generated_output_cannot_drop_real_contributing_pixels(tmp_path):
    reader,root,scope,*_=physical_fixture(generated=True,trace=True,tamper_generated=True)
    with pytest.raises(AdoptionInvalid,match='GENERATED'):
        verify_editor_effects_seal(reader,root,scope,scratch_root=tmp_path,max_bytes=80<<20,check_active=lambda:None)


def test_cancellation_after_reconstruction_input_io_does_not_accept_receipt(tmp_path):
    reader,root,scope,*_=physical_fixture(generated=True,trace=True)
    class Cancelled(RuntimeError):pass
    original=reader.iter_bytes
    cancelled=False
    def stream(ref,limit):
        nonlocal cancelled
        yield from original(ref,limit)
        cancelled=True
    reader.iter_bytes=stream
    def active():
        if cancelled:raise Cancelled('actor/session or lease no longer active')
    with pytest.raises(Cancelled):
        verify_editor_effects_seal(reader,root,scope,scratch_root=tmp_path,max_bytes=80<<20,check_active=active)
