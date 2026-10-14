import numpy as np
import pytest
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_replay_reader import RenderReplayReader
from photo_worker.v2.render_replay_artifacts import RenderReplayArtifacts
from photo_worker.v2.render_inputs import load_frozen_render_inputs
from photo_worker.v2.render_source import load_render_source_pixels
from test_v26_render_prepare_adoption import case
from test_layer_prepare import scratch


def test_existing_source_loader_reads_actual_hash_verified_owned_npy(tmp_path):
    reader,_,inputs=case();space=scratch(tmp_path,100000)
    artifacts=RenderReplayArtifacts(RenderReplayReader(reader),space)
    try:
        loaded=load_frozen_render_inputs(artifacts,inputs.recipe)
        with load_render_source_pixels(artifacts,loaded.source,max_input_bytes=100000) as prepared:
            assert not prepared.pixels.rgb.flags.writeable
            np.testing.assert_array_equal(prepared.pixels.rgb,np.zeros((2,2,3),np.float32))
            assert prepared.pixels.alpha is None
            assert list(space.directory.glob('*.npy'))
        assert reader.opened==reader.closed==1
        assert not list(space.directory.glob('*.npy'))
        assert not hasattr(artifacts,'stage') and not hasattr(artifacts,'stage_contract')
    finally:space.cleanup()


@pytest.mark.parametrize('kind',['corrupt','cancel','budget','consumer'])
def test_materialization_failures_release_scratch_and_streams(tmp_path,kind):
    reader,_,inputs=case();frozen=decode_frozen_render_recipe(reader.read_json(inputs.recipe,1048576))
    source=WorkingImage.model_validate_json(reader.objects[frozen.recipe.source.source.artifact_id][0])
    space=scratch(tmp_path,1 if kind=='budget' else 100000);seen=False
    def active():
        nonlocal seen
        if kind=='cancel' and list(space.directory.glob('*.npy')):
            seen=True;raise RuntimeError('cancelled')
    artifacts=RenderReplayArtifacts(RenderReplayReader(reader),space,active)
    if kind=='corrupt':
        identifier=source.pixels.artifact_id;body,media,scope,role=reader.objects[identifier]
        reader.objects[identifier]=(body[:-1]+b'x',media,scope,role)
    try:
        with pytest.raises(Exception):
            with artifacts.input(source.pixels) as value:
                if kind=='consumer':raise RuntimeError('consumer failed')
        assert reader.opened==reader.closed
        assert not list(space.directory.glob('*.npy'))
        if kind=='cancel':assert seen
    finally:space.cleanup()


def test_output_scope_and_invalid_suffix_cannot_materialize(tmp_path):
    reader,root,_=case();space=scratch(tmp_path)
    try:
        artifacts=RenderReplayArtifacts(RenderReplayReader(reader),space)
        with pytest.raises(AdoptionInvalid):
            with artifacts.input(root):pass
        with pytest.raises(AdoptionInvalid):
            with artifacts.input(root,suffix='../escape'):pass
        assert not list(space.directory.iterdir())
        assert reader.opened==reader.closed==0
    finally:space.cleanup()
