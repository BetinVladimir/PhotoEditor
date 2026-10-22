"""Deferred pure installed SQL binding tests; no fake approval/lease authority."""
from pathlib import Path
from copy import deepcopy
import json
import pytest

ROOT = Path(__file__).resolve().parents[2]


def binding_fixture(kind, fence=9223372036854775807):
    params = json.loads((ROOT / f'backend/internal/semanticreview/testdata/{kind}_preview_adoption_params.json').read_text())
    params['preview_fence'] = fence if fence <= 9007199254740991 else str(fence)
    preview = json.loads((ROOT / 'backend/internal/semanticreview/testdata/removal_preview_params.json').read_text())
    preview.update(tenant_id=params['tenant_id'],target_id=params['target_id'],kind='remove_object' if kind=='removal' else 'retouch_region')
    for field in ('model_lock_sha','runtime_lock_sha','device_profile_sha'):
        preview[field] = params[field]
    receipt = dict(schema_version=1,task_id=params['preview_task_id'],result_id=params['preview_result_id'],
        attempt=params['preview_attempt'],fence=fence,
        **{field:params[field] for field in ('presentation','numerical_evidence','working_image','pixels')})
    return params,preview,receipt


def bound(db,kind,params,preview,receipt):
    # Hard-coded kind allowlist; external strings never become SQL identifiers.
    assert kind in ('removal','retouch')
    sql = 'SELECT photo.semantic_shown_'+kind+'_params_bound(%s,%s,%s,%s,%s::jsonb,%s::jsonb,%s::jsonb,%s::jsonb)'
    args = (params['tenant_id'],params['child_run_id'],params['target_id'],params['preview_run_id'],
            json.dumps(params['approved_edit']),json.dumps(params),json.dumps(preview),json.dumps(receipt))
    return db.execute(sql,args).fetchone()[0]


@pytest.mark.parametrize('kind',['removal','retouch'])
@pytest.mark.parametrize('fence',[1,9007199254740991,9007199254740992,9223372036854775807])
def test_copy_binding_preserves_old_numeric_and_full_int64_fences(db,kind,fence):
    params,preview,receipt = binding_fixture(kind,fence)
    assert bound(db,kind,params,preview,receipt) is True


@pytest.mark.parametrize('kind',['removal','retouch'])
@pytest.mark.parametrize('defect',['unsafe_number','small_string','leading_zero','overflow','boolean','receipt_mismatch','wrong_task','ref_alias'])
def test_copy_binding_refuses_changed_or_noncanonical_fence_and_linkage(db,kind,defect):
    params,preview,receipt = binding_fixture(kind)
    assert bound(db,kind,params,preview,receipt) is True
    if defect=='unsafe_number': params['preview_fence']=9223372036854775807
    elif defect=='small_string': params['preview_fence']='1';receipt['fence']=1
    elif defect=='leading_zero': params['preview_fence']='09223372036854775807'
    elif defect=='overflow': params['preview_fence']='9223372036854775808'
    elif defect=='boolean': params['preview_fence']=True
    elif defect=='receipt_mismatch': receipt['fence']-=1
    elif defect=='wrong_task': params['preview_task_id']=params['preview_result_id']
    else: params['pixels']=deepcopy(params['working_image'])
    assert bound(db,kind,params,preview,receipt) is False
