from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_catalog_authority import verify_semantic_adoption_catalog
from test_semantic_source_authority import fixture


@pytest.mark.parametrize('variant',['valid','revoked','truthy','geometry','unprotected','cancel'])
def test_completed_result_catalog_scope_and_fail_closed_protection(tmp_path,variant):
    _,_,params,image,_,_,_=fixture(tmp_path)
    mask=MaskRef(artifact=params.intent,canvas=image.canvas,transform_chain=image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=variant!='unprotected')
    if variant=='geometry':mask.canvas.width+=1
    scope=(uuid4(),uuid4(),2,3,uuid4());calls=[]
    def execute(query,args):
        calls.append(args)
        assert 'v27_semantic_adoption_protection_origin' in query and args[:5]==scope
        return SimpleNamespace(fetchone=lambda:(1 if variant=='truthy' else variant!='revoked',))
    connection=SimpleNamespace(autocommit=True,execute=execute)
    def active():
        if variant=='cancel':raise InterruptedError()
    def run():
        verify_semantic_adoption_catalog(connection,*scope[:2],SimpleNamespace(attempt=2,fence=3,result_id=scope[4]),
            params,(image,image),(params.intent,params.calibration),(mask,mask),check_active=active)
    if variant=='valid':
        run();assert len(calls)==2 and calls[0][8] is False and calls[1][8] is True
    elif variant=='cancel':
        with pytest.raises(InterruptedError):run()
        assert calls==[]
    else:
        with pytest.raises(AdoptionInvalid):run()
        if variant in ('geometry','unprotected'):assert calls==[]


@pytest.mark.parametrize('variant',['valid','hash','revoked_after_io'])
def test_catalog_loader_hashes_inputs_and_rechecks_pins(tmp_path,variant):
    from copy import deepcopy
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.v2.adoption import ArtifactRef as ReadRef
    from photo_worker.v2.semantic_catalog_authority import load_semantic_adoption_catalog
    from test_v2_adoption import Reader
    _,_,params,image,_,_,_=fixture(tmp_path)
    params=deepcopy(params)
    reader=Reader()
    image_ref=ReadRef(uuid4(),canonical_hash(image));reader.add(image_ref,body=image,source='input')
    for binding in (params.reference,params.target):
        binding.source.artifact_id=image_ref.artifact_id;binding.source.sha256=image_ref.sha256
    mask=MaskRef(artifact=params.intent,canvas=image.canvas,transform_chain=image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=True)
    root=ReadRef(uuid4(),canonical_hash(mask));reader.add(root,body=mask,source='input')
    if variant=='hash':reader.body[image_ref]+=b' '
    reads=[]
    def execute(query,args):
        if 'adoption_protection(' in query:
            reads.append(query)
            rows=[] if len(reads)==2 and variant=='revoked_after_io' else [(root.artifact_id,root.sha256,root.artifact_id,root.sha256)]
            return SimpleNamespace(fetchall=lambda:rows)
        return SimpleNamespace(fetchone=lambda:(True,))
    conn=SimpleNamespace(autocommit=True,execute=execute)
    def run():return load_semantic_adoption_catalog(conn,uuid4(),uuid4(),SimpleNamespace(attempt=2,fence=3,result_id=uuid4()),reader,params)
    if variant=='valid':
        images,masks=run();assert images==(image,image) and masks==(mask,mask) and len(reads)==2
    else:
        with pytest.raises(AdoptionInvalid):run()
