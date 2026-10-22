"""Series transport expands to 5,000 sources without changing correction policy."""
from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.series import SeriesPackParams,SeriesStatsPack,PackedSeriesStats,SeriesParams
from photo_worker.operations.series.pack import execute as pack
from photo_worker.operations.series.balance import execute as balance
from test_series_balance import Artifacts,stats
from test_asset_adapters import context_for


def test_packed_and_direct_series_have_identical_quality_decisions():
    values=[stats([.4,.5,.6]),stats([.32,.4,.48]),stats([.3,.4,.5],.5)]
    direct=Artifacts(values,values[0].item_id);ctx=context_for(direct.value.stats[0],'series.balance');balance(ctx,direct)
    expected=canonical_bytes(direct.staged[-1][1])
    packed=Artifacts(values,values[0].item_id);refs=packed.value.stats;packed.value=SeriesPackParams(stats=refs)
    ctx=context_for(refs[0],'series.pack');result=pack(ctx,packed);batch=packed.staged[-1][1];packed.contracts[result.outputs[0].artifact_id]=batch
    packed.value=SeriesParams(scene_id='scene-one',stats_batches=result.outputs,reference_item_id=values[0].item_id)
    balance(context_for(result.outputs[0],'series.balance'),packed)
    assert canonical_bytes(packed.staged[-1][1])==expected


def test_five_thousand_series_items_use_at_most_sixty_three_input_packs():
    values=[stats([.4,.5,.6]) for _ in range(5000)]
    # No common region: exercise full inventory and bounded identity fallback,
    # without fitting 4,999 identical regressions in a transport regression.
    values[-1].regions[0].region_id='different-region'
    artifacts=Artifacts(values,values[0].item_id);refs=artifacts.value.stats;batches=[]
    for start in range(0,len(refs),80):
        artifacts.value=SeriesPackParams(stats=refs[start:start+80])
        result=pack(context_for(refs[start],'series.pack'),artifacts);batch=artifacts.staged[-1][1]
        assert len(canonical_bytes(batch))<1048576
        artifacts.contracts[result.outputs[0].artifact_id]=batch;batches.extend(result.outputs)
    assert len(batches)==63
    artifacts.value=SeriesParams(scene_id='scene-one',stats_batches=batches,reference_item_id=values[0].item_id)
    result=balance(context_for(batches[0],'series.balance'),artifacts);corrections=artifacts.staged[-1][1]
    assert len(corrections.items)==5000 and result.review_required
    assert 'no_common_regions' in corrections.warnings
    assert all(i.applied_ev==0 and i.applied_gains==[1.,1.,1.] for i in corrections.items)


def test_series_packs_reject_mixed_duplicate_and_oversized_inputs():
    value=stats([.4,.5,.6]);ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    entry=PackedSeriesStats(source=ref,value=value)
    with pytest.raises(ValidationError):SeriesStatsPack(stats=[entry,entry])
    with pytest.raises(ValidationError):SeriesPackParams(stats=[ref]*81)
    with pytest.raises(ValidationError):SeriesParams(scene_id='one',stats=[ref,ref],stats_batches=[ref])
    with pytest.raises(ValidationError):SeriesParams(scene_id='one')
