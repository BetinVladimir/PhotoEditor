from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_layer_mask_inputs import fixture
from photo_worker.v2.render_masks import resolve_render_masks


def inputs(image,ref):
    from photo_worker.contracts.base import canonical_hash
    leaf={'op':'ref','mask':ref.model_dump(mode='json'),'canvas_hash':canonical_hash(image.canvas)}
    layers=[SimpleNamespace(id=uuid4(),enabled=True,opacity=1,mask=leaf) for _ in range(2)]
    return SimpleNamespace(source=image,layers=SimpleNamespace(layers=layers),recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None)),protection=None)


def test_shared_layer_masks_map_once_and_close_on_exit(tmp_path):
    image,_,ref,_,artifacts,_=fixture(tmp_path);state=inputs(image,ref)
    with resolve_render_masks(artifacts,state,max_mask_bytes=8200,max_tile_bytes=4096) as tiles:
        for layer in state.layers.layers:np.testing.assert_allclose(tiles.layer(layer.id,0,0,2,1),[[.2,.8]])
        assert artifacts.opens==1 and tiles.protection(0,0,2,1) is None
    assert artifacts.closes==1
    with pytest.raises(ValueError,match='CLOSED'):tiles.layer(state.layers.layers[0].id,0,0,2,1)


def test_mask_budget_is_checked_before_materialization(tmp_path):
    image,_,ref,_,artifacts,_=fixture(tmp_path)
    with pytest.raises(ValueError,match='INPUT_BUDGET'):
        with resolve_render_masks(artifacts,inputs(image,ref),max_mask_bytes=8199,max_tile_bytes=4096):pass
    assert artifacts.opens==0 and artifacts.contracts==0


def test_protection_and_layer_share_verified_array_and_cleanup_after_consumer_error(tmp_path):
    image,mask,ref,_,artifacts,_=fixture(tmp_path);mask.protected=True
    state=inputs(image,ref);state.recipe.recipe.protected_mask=ref;state.protection=mask
    with pytest.raises(RuntimeError,match='consumer'):
        with resolve_render_masks(artifacts,state,max_mask_bytes=8200,max_tile_bytes=4096) as tiles:
            np.testing.assert_allclose(tiles.protection(0,0,2,1),[[.2,.8]])
            assert artifacts.opens==1
            raise RuntimeError('consumer')
    assert artifacts.closes==1
    with pytest.raises(ValueError,match='CLOSED'):tiles.protection(0,0,2,1)
