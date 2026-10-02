"""Real SQL frozen composition shape checks; these do not authorize a graph."""
from copy import deepcopy
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from photo_worker.operations.layers.contracts import LayerComposeParams
from photo_worker.contracts.base import canonical_bytes


def params():
    ref=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    return dict(schema_version=1,base=dict(asset_version_id=str(uuid4()),source=ref(),canvas_hash='b'*64),
        source_result=None,proposal_result=ref(),protected_mask=None,
        mask=dict(op='ref',mask=ref(),canvas_hash='b'*64),opacity=.5,blend='normal')


@pytest.mark.parametrize('variant',['normal','multiply','screen','parents','depth16','tree255',
    'missing_source','missing_protection','extra','bool_opacity','large_opacity','bad_ref','null_base',
    'null_mask','canvas','extra_leaf','missing_operand','depth17','tree511','nil_version','bad_version'])
def test_v25_layer_sql_shape_matches_canonical_worker_contract(db,variant):
    body=params();valid=variant in {'normal','multiply','screen','parents','depth16','tree255'}
    if variant in {'normal','multiply','screen'}:body['blend']=variant
    elif variant=='parents':
        body['source_result']=deepcopy(body['proposal_result']);body['protected_mask']=deepcopy(body['mask']['mask'])
    elif variant in {'depth16','depth17'}:
        for _ in range(16 if variant=='depth16' else 17):body['mask']=dict(op='invert',operand=body['mask'])
    elif variant in {'tree255','tree511'}:
        for _ in range(7 if variant=='tree255' else 8):body['mask']=dict(op='union',left=body['mask'],right=deepcopy(body['mask']))
    elif variant=='missing_source':del body['source_result']
    elif variant=='missing_protection':del body['protected_mask']
    elif variant=='extra':body['unexpected']=True
    elif variant=='bool_opacity':body['opacity']=True
    elif variant=='large_opacity':body['opacity']=1.01
    elif variant=='bad_ref':body['proposal_result']['sha256']='invalid'
    elif variant=='null_base':body['base']=None
    elif variant=='null_mask':body['mask']=None
    elif variant=='canvas':body['mask']['canvas_hash']='c'*64
    elif variant=='extra_leaf':body['mask']['extra']=1
    elif variant=='missing_operand':body['mask']=dict(op='invert')
    elif variant=='nil_version':body['base']['asset_version_id']='00000000-0000-0000-0000-000000000000'
    elif variant=='bad_version':body['base']['asset_version_id']='invalid'
    # Nil UUID rejection is an admission-domain invariant beyond UUID parsing.
    if variant!='nil_version':
        if valid:LayerComposeParams.model_validate_json(canonical_bytes(body))
        else:
            with pytest.raises(ValueError):LayerComposeParams.model_validate_json(canonical_bytes(body))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.v25_layer_params_shape(%s)',(Jsonb(body),)).fetchone()[0] is valid


def test_v25_layer_shape_null_and_privileges(db):
    assert db.execute('SELECT photo.v25_layer_params_shape(NULL)').fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege('photo_api','photo.v25_layer_params_shape(jsonb)','EXECUTE')").fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.v25_layer_params_shape(jsonb)','EXECUTE')").fetchone()[0] is True
