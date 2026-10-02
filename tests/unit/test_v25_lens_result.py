from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.operations.optical.result import lens_working_image


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def test_lens_geometry_preserves_identity_and_explicit_field_convention():
    source=WorkingImage(pixels=ref(),alpha=None,dtype='float32',
        canvas=Canvas(width=4,height=3,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    pixels,alpha,field=ref(),ref(),ref()
    identity=lens_working_image(source,pixels,alpha,field,strength=0)
    assert identity.canvas==source.canvas and identity.transform_chain==source.transform_chain
    corrected=lens_working_image(source,pixels,alpha,field,strength=1)
    assert corrected.canvas.source_sha256==source.canvas.source_sha256
    assert corrected.canvas.space_id!=source.canvas.space_id
    step=corrected.transform_chain.steps[-1]
    assert step.pixel_centers=='half_pixel' and step.invertibility=='partial'
    assert step.inverse_field==field and step.from_canvas==source.canvas
    assert WorkingImage.model_validate_json(corrected.model_dump_json())==corrected
    assert lens_working_image(source,pixels,alpha,field,strength=.5).canvas==corrected.canvas
    assert lens_working_image(source,pixels,alpha,ref(),strength=1).canvas!=corrected.canvas
    with pytest.raises(ValueError):lens_working_image(source,pixels,alpha,field,strength=float('nan'))


def test_staged_lens_closure_and_zero_strength_reuse(tmp_path):
    import numpy as np
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.operations.optical.prepare import prepare_lens
    from photo_worker.operations.optical.result import stage_lens_result
    from photo_worker.runtime.scratch import ScratchSpace
    from test_v25_lens_map import profile
    source=WorkingImage(pixels=ref(),alpha=None,dtype='float32',
        canvas=Canvas(width=3,height=3,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    source_payload=canonical_bytes(source)
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source_payload).hexdigest());binding=VersionBinding(asset_version_id=uuid4(),source=source_ref,canvas_hash=canonical_hash(source.canvas))
    class Staging:
        def __init__(self):self.members={};self.roles=[];self.evidence_rows={};self.json_rows={};self.binary_rows={}
        def stage(self,path,reservation,role,media_type):
            reservation.validate_file(path);assert media_type=='application/x-npy'
            payload=path.read_bytes();result=ArtifactRef(artifact_id=uuid4(),sha256=sha256(payload).hexdigest());self.binary_rows[result.artifact_id]=payload;self.members[result.artifact_id]=np.load(path,allow_pickle=False);self.roles.append(role);self.evidence_rows[result.artifact_id]=(result,media_type,len(payload),role);return result
        def stage_contract(self,value,role):
            payload=canonical_bytes(value);result=ArtifactRef(artifact_id=uuid4(),sha256=sha256(payload).hexdigest());self.members[result.artifact_id]=value;self.roles.append(role);self.evidence_rows[result.artifact_id]=(result,'application/json',len(payload),role);self.json_rows[result.artifact_id]=payload;return result
    for strength in (0,1):
        with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
            with prepare_lens(profile(k=[.2,0,0]),np.ones((3,3,3),np.float32),np.ones((3,3),np.float32),strength,scratch) as files:
                staging=Staging();result=stage_lens_result(staging,source_ref,source,binding,files,strength=strength)
                document=staging.members[result.document.artifact_id]
                assert document.generated_mask is None
                if strength==0:
                    assert document.working_image==source_ref
                    assert staging.roles==['mask_pixels','mask','report','document']
                    assert not result.review_required
                else:
                    assert 'warp_field' in staging.roles and result.review_required
                    image=staging.members[document.working_image.artifact_id]
                    assert image.transform_chain.steps[-1].pixel_centers=='half_pixel'
                    from photo_worker.v2.adoption import prepare_v2_adoption,ArtifactRef as AdoptionRef,ArtifactEvidence
                    from photo_worker.operations.optical.contracts import OpticalLensParams
                    params_payload=canonical_bytes(OpticalLensParams(schema_version=1,base=binding,strength=strength,profile=ref(),focal_mm=50,aperture=2.8))
                    params_ref=AdoptionRef(uuid4(),sha256(params_payload).hexdigest())
                    inputs={source_ref.artifact_id:source_payload,params_ref.artifact_id:params_payload}
                    class Reader:
                        def evidence(self,requested):
                            if requested.artifact_id in inputs:
                                return ArtifactEvidence(requested,'application/json',len(inputs[requested.artifact_id]),None,'input')
                            value,media,size,role=staging.evidence_rows[requested.artifact_id]
                            return ArtifactEvidence(AdoptionRef(value.artifact_id,value.sha256),media,size,role,'output')
                        def read_json(self,requested,limit):return inputs[requested.artifact_id] if requested.artifact_id in inputs else staging.json_rows[requested.artifact_id]
                        def iter_bytes(self,requested,limit):
                            payload=staging.binary_rows[requested.artifact_id]
                            for offset in range(0,len(payload),13):yield payload[offset:offset+13]
                        def staged_outputs(self):return {AdoptionRef(value.artifact_id,value.sha256) for value,media,size,role in staging.evidence_rows.values()}
                    from photo_worker.v2.adoption import _load_registry,SCHEMA_PATH
                    from jsonschema import Draft202012Validator
                    loaded=_load_registry(SCHEMA_PATH)
                    for member in staging.members.values():
                        if hasattr(member,'model_dump'):
                            validation=Draft202012Validator({**loaded,'$ref':'#/$defs/'+type(member).__name__})
                            errors=list(validation.iter_errors(member.model_dump(mode='json')))
                            assert not errors, (type(member).__name__,[str(e) for e in errors])
                    prepared=prepare_v2_adoption(task_id=uuid4(),operation='optical.lens',version='1',
                        root=AdoptionRef(result.document.artifact_id,result.document.sha256),reader=Reader(),params_ref=params_ref)
                    assert len(prepared.inventory)==len(result.outputs)+1
                    from photo_worker.v2.lens_adoption import verify_lens_correction
                    from photo_worker.v2.adoption import AdoptionInvalid
                    from photo_worker.operations.optical.contracts import ImageReport
                    # Valid JSON/SHA with a substituted provenance report must fail.
                    original_report=staging.members[document.report.artifact_id]
                    forged_report=original_report.model_copy(update={'sources':[binding.model_copy(update={'asset_version_id':uuid4()})]})
                    forged_report_ref=staging.stage_contract(forged_report,'report')
                    forged_root=staging.stage_contract(document.model_copy(update={'report':forged_report_ref}),'document')
                    with pytest.raises(AdoptionInvalid,match='REPORT_INVALID'):
                        verify_lens_correction(Reader(),params_ref,AdoptionRef(forged_root.artifact_id,forged_root.sha256))
                    # Valid image document with another color profile cannot be adopted.
                    altered_image=image.model_copy(update={'color':source.color.model_copy(update={'icc_sha256':'d'*64})})
                    altered_image_ref=staging.stage_contract(altered_image,'working_image')
                    altered_root=staging.stage_contract(document.model_copy(update={'working_image':altered_image_ref}),'document')
                    with pytest.raises(AdoptionInvalid,match='GEOMETRY_INVALID'):
                        verify_lens_correction(Reader(),params_ref,AdoptionRef(altered_root.artifact_id,altered_root.sha256))
                bad=binding.model_copy(update={'canvas_hash':'d'*64})
                rejected=Staging()
                with pytest.raises(ValueError,match='LENS_SOURCE_MISMATCH'):
                    stage_lens_result(rejected,source_ref,source,bad,files,strength=strength)
                assert rejected.roles==[]
