from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain,MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.layers.contracts import LayerComposeParams
from photo_worker.operations.layers.parents import resolve_layer_parents


def setup(tmp_path):
    def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=WorkingImage(pixels=ref(),alpha=None,dtype='float32',canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    sr,pr,sp,pp,mr=ref(),ref(),ref(),ref(),ref()
    mask=MaskRef(artifact=ref(),canvas=source.canvas,transform_chain=source.transform_chain,selector='generated',confidence=1,model_lock_id=None,protected=False)
    parent=lambda image:ImageResult(schema_version=1,working_image=image,report=ref(),changed_mask=ref(),generated_mask=mr)
    documents={sr.artifact_id:source,pr.artifact_id:source.model_copy(deep=True),sp.artifact_id:parent(sr),pp.artifact_id:parent(pr),mr.artifact_id:mask}
    params=LayerComposeParams(schema_version=1,base=VersionBinding(asset_version_id=uuid4(),source=sr,canvas_hash=canonical_hash(source.canvas)),source_result=sp,proposal_result=pp,mask={'op':'ref','mask':mr.model_dump(mode='json'),'canvas_hash':canonical_hash(source.canvas)},protected_mask=None,opacity=1,blend='normal')
    for parent_doc in (documents[sp.artifact_id],documents[pp.artifact_id]):
        documents[parent_doc.report.artifact_id]=ImageReport(schema_version=1,status='pass',operation='fixture',skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[params.base],registrations=[])
    path=tmp_path/'coverage.npy';np.save(path,np.array([[0,1]],np.float32),allow_pickle=False)
    class Artifacts:
        opens=0;closes=0
        def contract(self,reference,model):
            value=documents[reference.artifact_id];assert isinstance(value,model);return value
        @contextmanager
        def input(self,reference,**kwargs):
            assert reference==mask.artifact;self.opens+=1
            try:yield SimpleNamespace(path=path)
            finally:self.closes+=1
    return params,documents,Artifacts(),path


def test_shared_coverage_verified_once_bounds_and_closed_reader(tmp_path):
    params,documents,artifacts,path=setup(tmp_path)
    with resolve_layer_parents(params,artifacts) as parents:
        assert artifacts.opens==1
        np.testing.assert_array_equal(parents.read_source_generated(0,0,2,1),[[0,1]])
        np.testing.assert_array_equal(parents.read_proposal_generated(1,0,1,1),[[1]])
        with pytest.raises(ValueError,match='TILE_INVALID'):parents.read_source_generated(True,0,1,1)
    assert artifacts.closes==1
    with pytest.raises(ValueError,match='CLOSED'):parents.read_source_generated(0,0,1,1)


def test_parent_image_substitution_and_geometry_refused_before_io(tmp_path):
    params,documents,artifacts,path=setup(tmp_path)
    parent=documents[params.source_result.artifact_id]
    original=parent.working_image
    parent.working_image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    with pytest.raises(ValueError,match='SOURCE_PARENT_INVALID'):
        with resolve_layer_parents(params,artifacts):pytest.fail('substituted source')
    parent.working_image=original
    proposal=documents[documents[params.proposal_result.artifact_id].working_image.artifact_id]
    proposal.color=proposal.color.model_copy(update={'icc_sha256':'d'*64})
    with pytest.raises(ValueError,match='GEOMETRY_INVALID'):
        with resolve_layer_parents(params,artifacts):pytest.fail('different color')
    assert artifacts.opens==0


def test_nonbinary_lineage_and_consumer_failure_cleanup(tmp_path):
    params,documents,artifacts,path=setup(tmp_path)
    np.save(path,np.array([[0,.5]],np.float32),allow_pickle=False)
    with pytest.raises(ValueError,match='NOT_BINARY'):
        with resolve_layer_parents(params,artifacts):pytest.fail('probability lineage')
    assert artifacts.opens==artifacts.closes==1
    np.save(path,np.array([[0,1]],np.float32),allow_pickle=False)
    with pytest.raises(RuntimeError,match='consumer'):
        with resolve_layer_parents(params,artifacts) as parents:raise RuntimeError('consumer')
    assert artifacts.opens==artifacts.closes==2
    with pytest.raises(ValueError,match='CLOSED'):parents.read_proposal_generated(0,0,1,1)


def test_original_without_generated_parent(tmp_path):
    params,documents,artifacts,path=setup(tmp_path)
    params.source_result=None
    documents[params.proposal_result.artifact_id].generated_mask=None
    with resolve_layer_parents(params,artifacts) as parents:
        assert parents.source_result is None and parents.read_source_generated is None and parents.read_proposal_generated is None
    assert artifacts.opens==0


@pytest.mark.parametrize('status',['fail','unavailable'])
def test_rejected_parent_report_prevents_array_io(tmp_path,status):
    params,documents,artifacts,path=setup(tmp_path)
    parent=documents[params.proposal_result.artifact_id]
    documents[parent.report.artifact_id].status=status
    with pytest.raises(ValueError,match='PARENT_REPORT_REJECTED'):
        with resolve_layer_parents(params,artifacts):pytest.fail('rejected parent admitted')
    assert artifacts.opens==0


@pytest.mark.parametrize('which',['source_result','proposal_result'])
def test_foreign_version_report_rejected_before_arrays(tmp_path,which):
    params,documents,artifacts,path=setup(tmp_path)
    parent=documents[getattr(params,which).artifact_id]
    report=documents[parent.report.artifact_id]
    report.sources=[params.base.model_copy(update={'asset_version_id':uuid4()})]
    with pytest.raises(ValueError,match='REPORT_SOURCE_INVALID'):
        with resolve_layer_parents(params,artifacts):pytest.fail('foreign report admitted')
    assert artifacts.opens==0
