"""Pure SQL inventory projection; no authority/physical/activation claim."""
from copy import deepcopy
from uuid import uuid4, UUID

import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config


def inventory_fixture(alpha=False, geometry=False):
    def ref():
        return dict(artifact_id=str(uuid4()),sha256='a'*64)
    params = dict(plan=ref(),intent=ref(),candidate=ref(),source=dict(source=ref()),protection=ref())
    param_ref = ref()
    node = dict(params_artifact_id=param_ref['artifact_id'],params_sha=param_ref['sha256'])
    candidate = dict(mask=ref(),transform=ref())
    intent = dict(reference=dict(source=ref()))
    source = dict(pixels=ref(),transform_chain=dict(steps=[]))
    reference = dict(pixels=ref(),transform_chain=dict(steps=[]))
    mask, protection = dict(artifact=ref()),dict(artifact=ref())
    roots = [(param_ref,'Params'),(params['plan'],'SemanticPlan'),(params['intent'],'EditIntent'),
        (params['candidate'],'TransferCandidate'),(params['source']['source'],'WorkingImage'),
        (params['protection'],'MaskRef'),(candidate['mask'],'MaskRef'),(candidate['transform'],'ResolvedTransform'),
        (intent['reference']['source'],'WorkingImage'),(source['pixels'],'NpyArray'),
        (reference['pixels'],'NpyArray'),(mask['artifact'],'NpyArray'),(protection['artifact'],'NpyArray')]
    for image in (source,reference):
        if alpha:
            image['alpha'] = ref()
            roots.append((image['alpha'],'NpyArray'))
        if geometry:
            step = dict(kind='dense_warp',inverse_field=ref())
            image['transform_chain']['steps'].append(step)
            roots.append((step['inverse_field'],'NpyArray'))
    entries = [dict(ref=deepcopy(r),contract=c) for r,c in roots]
    entries.sort(key=lambda entry: UUID(entry['ref']['artifact_id']).int)
    return [params,node,dict(entries=entries),candidate,intent,source,reference,mask,protection]


def exact(db,args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.semantic_background_cleanup_preview_inventory_exact(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                            tuple(Jsonb(value) for value in args)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('alpha,geometry', [(False,False),(True,False),(False,True),(True,True)])
def test_background_cleanup_inventory_includes_optional_alpha_geometry(database,alpha,geometry):
    args = inventory_fixture(alpha,geometry)
    assert exact(database,args) is True
    assert len(args[2]['entries']) == 13+2*int(alpha)+2*int(geometry)
    for index in range(len(args[2]['entries'])):
        changed = deepcopy(args)
        del changed[2]['entries'][index]
        assert exact(database,changed) is False


@pytest.mark.parametrize('mutation', ['extra','duplicate','order','contract','sha','cross_contract_alias'])
def test_background_cleanup_inventory_rejects_unowned_projection(database,mutation):
    args = inventory_fixture(True,True)
    entries = args[2]['entries']
    if mutation == 'extra':
        entries.append(dict(ref=dict(artifact_id=str(uuid4()),sha256='a'*64),contract='NpyArray'))
        entries.sort(key=lambda e: UUID(e['ref']['artifact_id']).int)
    elif mutation == 'duplicate':
        entries.insert(0,deepcopy(entries[0]))
    elif mutation == 'order':
        entries.reverse()
    elif mutation == 'contract':
        entries[0]['contract'] = 'ForeignContract'
    elif mutation == 'sha':
        entries[0]['ref']['sha256'] = 'b'*64
    else:
        # A shared artifact ID cannot serve different contracts in the closure.
        args[0]['plan'] = deepcopy(args[0]['intent'])
        for entry in entries:
            if entry['contract'] == 'SemanticPlan':
                entry['ref'] = deepcopy(args[0]['intent'])
        entries.sort(key=lambda entry: UUID(entry['ref']['artifact_id']).int)
    assert exact(database,args) is False
