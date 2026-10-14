from copy import deepcopy
from types import SimpleNamespace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.operations.semantic.source_authority import semantic_input_authority
from test_semantic_transfer_contracts import transfer_documents
from test_image_denoise import Artifacts


def fixture(tmp_path):
    artifacts=Artifacts(tmp_path);params,_,_=transfer_documents()
    params=params.model_copy(update={'reference':params.reference.model_copy(update={'canvas_hash':canonical_hash(artifacts.image.canvas)}),'target':params.target.model_copy(update={'canvas_hash':canonical_hash(artifacts.image.canvas)})})
    raw=canonical_bytes(params);lease=SimpleNamespace(command=SimpleNamespace(params=SimpleNamespace(sha256=sha256(raw).hexdigest())))
    class Database:
        calls=[]
        accepted=True
        def semantic_source_ready(self,owner,binding,*,target):
            self.calls.append((deepcopy(binding),target))
            return self.accepted
    db=Database();colors=[]
    verify=semantic_input_authority(db,lease,raw,verify_color=lambda image:colors.append(image))
    return db,colors,params,artifacts.image,lease,raw,verify


def test_owned_params_and_exact_reference_target_sql_order(tmp_path):
    db,colors,params,image,lease,_,verify=fixture(tmp_path)
    lease.command.params.sha256='0'*64
    verify(params,image,image)
    assert db.calls==[(params.reference,False),(params.target,True)] and len(colors)==2


def test_changed_params_rejected_before_scope_or_color(tmp_path):
    db,colors,params,image,_,_,verify=fixture(tmp_path)
    changed=params.model_copy(update={'model_lock_sha':'0'*64})
    with pytest.raises(AdoptionInvalid,match='PARAMS_CHANGED'):verify(changed,image,image)
    assert db.calls==[] and colors==[]


def test_revoked_source_and_changed_canvas_stop_before_color(tmp_path):
    db,colors,params,image,_,_,verify=fixture(tmp_path)
    db.accepted=False
    with pytest.raises(AdoptionInvalid,match='SCOPE_STALE'):verify(params,image,image)
    assert colors==[]
    db.calls=[]
    changed=params.model_copy(update={'reference':params.reference.model_copy(update={'canvas_hash':'0'*64})})
    raw=canonical_bytes(changed)
    lease=SimpleNamespace(command=SimpleNamespace(params=SimpleNamespace(sha256=sha256(raw).hexdigest())))
    verify=semantic_input_authority(db,lease,raw,verify_color=lambda value:colors.append(value))
    with pytest.raises(AdoptionInvalid,match='SOURCE_CHANGED'):verify(changed,image,image)
    assert db.calls==[] and colors==[]


def test_raw_params_hash_and_duplicate_keys_denied(tmp_path):
    db,_,_,_,lease,raw,_=fixture(tmp_path)
    with pytest.raises(AdoptionInvalid):semantic_input_authority(db,lease,raw+b' ',verify_color=lambda value:None)
    raw=raw[:-1]+b',"schema_version":1}'
    lease.command.params.sha256=sha256(raw).hexdigest()
    with pytest.raises(ValueError):semantic_input_authority(db,lease,raw,verify_color=lambda value:None)


@pytest.mark.parametrize('variant',['valid','missing','changed','revoked_after_origin','origin_denied'])
def test_frozen_protection_authority_is_owned_and_rechecked(tmp_path,variant):
    from photo_worker.operations.semantic.load_inputs import sql_frozen_semantic_array_loader
    db,colors,params,image,lease,raw,_=fixture(tmp_path)
    ref=params.reference.source
    record={'source_protection':ref.model_dump(mode='json'),'target_protection':ref.model_dump(mode='json')}
    db.semantic_protection_pins=lambda owner:deepcopy(record)
    origins=[]
    def origin(*values):
        origins.append(values)
        if variant=='origin_denied':raise AdoptionInvalid('ORIGIN_DENIED')
        if variant=='revoked_after_origin':db.semantic_protection_pins=lambda owner:None
    loader=sql_frozen_semantic_array_loader(db,lease,raw,verify_color=colors.append,verify_protection=origin)
    lease.command.params.sha256='0'*64
    if variant=='missing':db.semantic_protection_pins=lambda owner:None
    if variant=='changed':record['target_protection']['sha256']='0'*64
    if variant=='valid':
        loader.verify_inputs(params,image,image)
        assert len(origins)==1 and len(colors)==2
        assert loader.target_protection==ref
    else:
        with pytest.raises(AdoptionInvalid):loader.verify_inputs(params,image,image)
        if variant in ('missing','changed'):assert origins==[] and colors==[]


def test_frozen_protection_requires_explicit_origin_verifier(tmp_path):
    from photo_worker.operations.semantic.load_inputs import sql_frozen_semantic_array_loader
    db,_,_,_,lease,raw,_=fixture(tmp_path)
    with pytest.raises(ValueError,match='PROTECTION_AUTHORITY_INVALID'):
        sql_frozen_semantic_array_loader(db,lease,raw,verify_color=lambda value:None,verify_protection=None)
