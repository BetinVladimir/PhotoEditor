from contextlib import contextmanager
import hashlib,json
import pytest
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.report import ContactSheet,ContactSheetParams,PreviewEntry
from photo_worker.operations.report.contact_sheet import execute
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for


ROOT=Path(__file__).resolve().parents[2]


class Artifacts:
    def __init__(self,root):
        self.paths={};self.pages=[];self.staged=[];entries=[]
        for i in range(6):
            path=root/f'{i}.jpg';Image.new('RGB',(80+i*5,60),(20*i,80,140)).save(path,quality=90);r=self.ref(path);self.paths[r.artifact_id]=path;entries.append(PreviewEntry(preview=r,media_type='image/jpeg',label=f'Фото {i+1}'))
        font_path=ROOT/'backend/internal/contactsheet/NotoSans-Regular.ttf';self.font=self.ref(font_path);self.paths[self.font.artifact_id]=font_path
        self.value=ContactSheetParams(entries=entries,locale='ru',font=self.font,font_lock_id=self.font.sha256,columns=2,rows=2)
    def ref(self,path):return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    def params(self,model):return model.model_validate_json(json.dumps(self.value.model_dump(mode='json')))
    @contextmanager
    def input(self,r,*,suffix):yield SimpleNamespace(path=self.paths[r.artifact_id])
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path);Image.open(path).verify();r=self.ref(path);self.pages.append(r);return r
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def test_contact_sheet_paginates_and_binds_font(tmp_path):
    a=Artifacts(tmp_path);ctx=context_for(a.value.entries[0].preview,'report.contact_sheet');ctx.lease.command.inputs=[*[e.preview for e in a.value.entries],a.font];root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(ctx,a)
    manifest=a.staged[0][1];ContactSheet.model_validate_json(canonical_bytes(manifest))
    assert manifest.total_items==6 and [p.item_count for p in manifest.pages]==[4,2]
    assert [p.first_index for p in manifest.pages]==[0,4] and len(result.outputs)==3


def test_cancelled_contact_sheet_does_not_read_parameters():
    def cancelled():
        raise RuntimeError('lease lost')
    def forbidden(_model):
        pytest.fail('cancelled task read parameters')
    with pytest.raises(RuntimeError,match='lease lost'):
        execute(SimpleNamespace(check_active=cancelled),SimpleNamespace(params=forbidden))


def test_contact_sheet_lease_loss_during_decode_stages_nothing(tmp_path,monkeypatch):
    import importlib
    operation=importlib.import_module('photo_worker.operations.report.contact_sheet')
    artifacts=Artifacts(tmp_path)
    ctx=context_for(artifacts.value.entries[0].preview,'report.contact_sheet')
    ctx.lease.command.inputs=[*[e.preview for e in artifacts.value.entries],artifacts.font]
    original_fit=operation._fit
    cancelled=False
    def active():
        if cancelled:
            raise RuntimeError('lease lost')
    def fit(*args):
        nonlocal cancelled
        result=original_fit(*args)
        cancelled=True
        return result
    monkeypatch.setattr(ctx,'check_active',active)
    monkeypatch.setattr(operation,'_fit',fit)
    # No scratch implementation: cancellation must occur before staging any page.
    with pytest.raises(RuntimeError,match='lease lost'):
        execute(ctx,artifacts)
    assert not artifacts.pages and not artifacts.staged
