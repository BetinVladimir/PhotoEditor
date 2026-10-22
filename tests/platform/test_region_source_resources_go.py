"""Compare actual server and worker full-source resource estimators."""
import json
from go_parity_owner import go_parity_owner
from photo_worker.operations.region.resources import region_source_resources


def test_server_source_budget_matches_worker_legacy_alpha_policy():
    cases=[]
    for width,height in [(1,1),(400,300),(513,511),(511,513),(6000,4000),(1,200000000),(200000000,1),(20000,10000)]:
        for alpha in (False,True):
            budget=region_source_resources(width,height,has_alpha=alpha,alpha_mode='normalize_uint16')
            cases.append(dict(Width=width,Height=height,Alpha=alpha,PixelCount=budget['pixel_count'],RAMBytes=budget['ram_bytes'],ScratchBytes=budget['scratch_bytes']))
    with go_parity_owner(__file__, {'PHOTO_REGION_SOURCE_BUDGETS':json.dumps(cases)}) as owner:
        result=owner.run(['go','test','./internal/v2region','-run','^TestRegionSourceBudgetWorkerParity$','-count=1'],timeout=30)
    assert result.returncode==0,result.stdout+result.stderr
