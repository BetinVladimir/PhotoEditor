import pytest
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
from photo_worker.v2.render_replay_reader import RenderReplayReader
from test_v26_render_prepare_adoption import case


def source(reader):
    identifier,value=next((key,value) for key,value in reader.objects.items()
        if value[1]=='application/x-npy' and value[2]=='input')
    from hashlib import sha256
    return ArtifactRef(identifier,sha256(value[0]).hexdigest())


def test_declared_recipe_and_binary_stream_hashes_and_closure():
    reader,_,inputs=case();view=RenderReplayReader(reader);ref=source(reader)
    assert view.read_json(inputs.recipe,1048576)==reader.objects[inputs.recipe.artifact_id][0]
    assert b''.join(view.iter_bytes(ref,100000))==reader.objects[ref.artifact_id][0]
    assert reader.opened==reader.closed==1


@pytest.mark.parametrize('kind',['corrupt','short','oversize','type','empty','cancel'])
def test_bad_or_cancelled_stream_never_proves_input_and_always_closes(kind):
    reader,_,_=case();ref=source(reader);original=reader.iter_bytes
    def modified(reference,limit):
        for index,block in enumerate(original(reference,limit)):
            if index==0 and kind=='corrupt':block=bytes([block[0]^1])+block[1:]
            if kind=='short':block=block[:-1]
            if index==0 and kind=='oversize':block=b'x'*65537
            if index==0 and kind=='type':block=bytearray(block)
            if index==0 and kind=='empty':block=b''
            yield block
    reader.iter_bytes=modified;calls=0
    def active():
        nonlocal calls
        calls+=1
        if kind=='cancel' and calls==3:raise RuntimeError('cancelled')
    with pytest.raises((AdoptionInvalid,RuntimeError)):
        list(RenderReplayReader(reader,active).iter_bytes(ref,100000))
    assert reader.opened==reader.closed==1


def test_early_close_aborts_underlying_versioned_input_stream():
    reader,_,_=case();ref=source(reader)
    stream=RenderReplayReader(reader).iter_bytes(ref,100000)
    assert next(stream)
    stream.close()
    assert reader.opened==reader.closed==1


@pytest.mark.parametrize('limit',[0,-1,True,1,2**53])
def test_binary_bound_is_checked_before_any_object_stream(limit):
    reader,_,_=case();ref=source(reader)
    with pytest.raises(AdoptionInvalid,match='BOUND_INVALID'):
        list(RenderReplayReader(reader).iter_bytes(ref,limit))
    assert reader.opened==reader.closed==0


@pytest.mark.parametrize('kind',['corrupt','wrong_type','oversize_limit','short_bound','binary_media'])
def test_json_limits_and_pin_are_independent_of_reader_claims(kind):
    reader,_,inputs=case();view=RenderReplayReader(reader);ref=inputs.recipe;limit=1048576
    original=reader.read_json
    if kind=='corrupt':reader.read_json=lambda *args:b' '+original(*args)[1:]
    elif kind=='wrong_type':reader.read_json=lambda *args:bytearray(original(*args))
    elif kind=='oversize_limit':limit=2**53
    elif kind=='short_bound':limit=1
    else:ref=source(reader)
    with pytest.raises(AdoptionInvalid):view.read_json(ref,limit)
    assert reader.opened==reader.closed==0
