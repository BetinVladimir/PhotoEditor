from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_plan import read_interchange_plan


def fixture():
    ref=lambda:{'artifact_id':str(uuid4()),'sha256':'a'*64}
    entry={'entry_id':str(uuid4()),'external_id':'frame-1','path':'originals/A.CR3','content':ref(),
           'binding':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},
           'recipe':ref(),'rating':3,'pick':'picked','crop':None}
    return {'schema_version':1,'project_id':str(uuid4()),'direction':'import','format':'photoeditor_bundle',
            'snapshot_digest':'c'*64,'entries':[entry],'max_archive_bytes':1<<30,
            'max_expanded_bytes':2<<30,'max_files':100}


def read(value,**changes):
    from uuid import UUID
    return read_interchange_plan(canonical_bytes(value),project_id=changes.get('project_id',UUID(value['project_id'])),
        direction=changes.get('direction','import'),snapshot_digest=changes.get('snapshot_digest','c'*64))


def test_plan_preserves_metadata_and_canonical_binding():
    value=fixture();plan=read(value);value['entries'][0]['rating']=0
    assert plan.entries[0].rating==3
    assert plan.entries[0].binding.canvas_hash=='b'*64


@pytest.mark.parametrize('field',['entry_id','external_id','path'])
def test_plan_rejects_colliding_identity_or_casefold_path(field):
    value=fixture();entry=dict(value['entries'][0]);entry.update(entry_id=str(uuid4()),external_id='frame-2',path='originals/B.CR3')
    entry[field]=value['entries'][0][field].lower() if field=='path' else value['entries'][0][field]
    value['entries'].append(entry)
    with pytest.raises(ValueError,match='DUPLICATE'):read(value)


@pytest.mark.parametrize('changes',[{'project_id':uuid4()},{'direction':'export'},{'snapshot_digest':'d'*64}])
def test_plan_rejects_foreign_frozen_scope(changes):
    with pytest.raises(ValueError,match='SCOPE'):read(fixture(),**changes)


@pytest.mark.parametrize('path', ['../escape', 'a\\b', 'C:photo', 'a//b'])
def test_plan_rejects_unsafe_paths(path):
    value=fixture();value['entries'][0]['path']=path
    with pytest.raises(ValueError):read(value)


def test_plan_rejects_duplicate_json_keys():
    value=fixture();raw=canonical_bytes(value).replace(b'"schema_version":1',b'"schema_version":1,"schema_version":1')
    from uuid import UUID
    with pytest.raises(ValueError,match='Duplicate'):
        read_interchange_plan(raw,project_id=UUID(value['project_id']),direction='import',snapshot_digest='c'*64)


def test_all_unsupported_import_keeps_entries_without_fake_references():
    value=fixture();value['entries'][0].update(binding=None,recipe=None)
    plan=read(value)
    assert len(plan.entries)==1 and plan.entries[0].binding is None and plan.entries[0].recipe is None


@pytest.mark.parametrize('field',['binding','recipe'])
def test_export_requires_real_binding_and_recipe(field):
    value=fixture();value['direction']='export';value['entries'][0][field]=None
    with pytest.raises(ValueError):read(value,direction='export')


def test_import_cannot_supply_recipe_for_uningested_source():
    value=fixture();value['entries'][0]['binding']=None
    with pytest.raises(ValueError,match='RECIPE_WITHOUT_BINDING'):read(value)


def test_ingested_import_without_recipe_remains_reportable():
    value=fixture();value['entries'][0]['recipe']=None
    assert read(value).entries[0].binding is not None
