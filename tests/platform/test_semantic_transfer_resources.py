import json
from pathlib import Path
import pytest
from photo_worker.operations.semantic.resources import semantic_transfer_resources,DOCUMENT_LIMIT
from photo_worker.operations.semantic.prepare_transfer import planar_transfer_resources


def test_transfer_publication_cost_covers_normative_role_limits():
    root=Path(__file__).resolve().parents[2]
    operation=next(item for item in json.loads((root/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
        if item['id']=='semantic.transfer')
    cost=semantic_transfer_resources((8,8),(10000,20000))
    assert cost['output_bytes']==operation['max_output_bytes']
    for role in ('mask','transform','document'):
        assert operation['role_policies'][role]['max_bytes']==DOCUMENT_LIMIT
    base=planar_transfer_resources((8,8),(10000,20000))
    assert cost['scratch_bytes']==base['scratch_bytes']+DOCUMENT_LIMIT
    assert cost['ram_bytes']==base['ram_bytes']


@pytest.mark.parametrize('shape',[(7,8),(True,8),(10001,20000)])
def test_transfer_cost_rejects_invalid_or_oversized_canvas(shape):
    with pytest.raises(ValueError):semantic_transfer_resources((8,8),shape)
