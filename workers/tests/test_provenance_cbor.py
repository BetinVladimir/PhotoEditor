"""RFC8949 subset and parser resource boundaries; no signature verification."""
import pytest

from photo_worker.operations.provenance.cbor import CBORTag,decode_cbor,encode_cbor_bytes


@pytest.mark.parametrize('raw,expected',[
    (b'\x00',0),(b'\x20',-1),(b'\x18\x18',24),(b'\x19\x01\x00',256),
    (b'\x62hi','hi'),(b'\x42hi',b'hi'),(b'\x83\xf4\xf5\xf6',[False,True,None]),
    (b'\xa2\x01\x26\x61x\x02',{1:-7,'x':2}),
    (b'\xf9\x3c\x00',1.0)])
def test_supported_values(raw,expected):assert decode_cbor(raw)==expected


@pytest.mark.parametrize('raw',[
    b'\xa2\x01\x00\x01\x01', # duplicate int key
    b'\xa1\xf5\x00', # bool map key cannot alias integer1
    b'\xa1\x80\x00', # array map key
    b'\x18\x01', # nonpreferred integer
    b'\x9f\xff', # indefinite length
    b'\x63ab', # truncated string
    b'\x61\xff', # invalid UTF8
    b'\x00\x00', # trailing item
    b'\xf9\x7c\x00', # infinity
    b'\xf9\x7e\x00', # NaN
    b'\xf7', # undefined
    b'\xc0\x00', # unapproved tag
    b'\x81'*66+b'\x00', # depth bound
    b'\x9a\x00\x01\x86\xa1', # array count100001
])
def test_malformed_ambiguous_or_unbounded_values_refused(raw):
    with pytest.raises(ValueError):decode_cbor(raw)


def test_only_explicit_tag18_allowed():
    assert decode_cbor(b'\xd2\x80',allowed_tags=(18,))==CBORTag(18,[])
    with pytest.raises(ValueError):decode_cbor(b'\xd2\x80')


@pytest.mark.parametrize('size',[0,23,24,255,256,65535,65536])
def test_bytes_encoding_round_trip(size):
    value=b'x'*size
    assert decode_cbor(encode_cbor_bytes(value))==value


def test_cancellation_is_observed_during_tree_walk():
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):decode_cbor(b'\x83\x00\x01\x02',check_active=active)


def test_boolean_denial_refuses_cbor_before_and_after_tree_walk():
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        decode_cbor(b'\x00',check_active=lambda:False)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        return calls<3
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        decode_cbor(b'\x00',check_active=active)


def test_core_deterministic_map_order_is_encoded_byte_lexicographic():
    # uint24 encodes 1818; negative1 encodes20. Core byte order puts uint24
    # first, whereas legacy length-first ordering would put negative1 first.
    raw=b'\xa2\x18\x18\x00\x20\x01'
    assert decode_cbor(raw,deterministic=True)=={24:0,-1:1}
    reverse=b'\xa2\x20\x01\x18\x18\x00'
    assert decode_cbor(reverse)=={-1:1,24:0}
    with pytest.raises(ValueError,match='SIGNER_CBOR_MAP_ORDER'):
        decode_cbor(reverse,deterministic=True)


def test_nested_map_order_is_checked():
    with pytest.raises(ValueError,match='SIGNER_CBOR_MAP_ORDER'):
        decode_cbor(b'\x81\xa2\x61z\x00\x61a\x01',deterministic=True)


@pytest.mark.parametrize('raw',[b'\xfa\x3f\x80\x00\x00',
    b'\xfb\x3f\xf0\x00\x00\x00\x00\x00\x00',
    b'\xfa\x80\x00\x00\x00'])
def test_overwide_float_rejected_only_in_deterministic_mode(raw):
    assert isinstance(decode_cbor(raw),float)
    with pytest.raises(ValueError,match='SIGNER_CBOR_NONPREFERRED_FLOAT'):
        decode_cbor(raw,deterministic=True)


def test_preferred_negative_zero_preserved():
    from math import copysign
    value=decode_cbor(b'\xf9\x80\x00',deterministic=True)
    assert value==0 and copysign(1,value)==-1


def test_float_requiring_single_precision_accepted():
    from struct import pack
    value=100000.0
    assert decode_cbor(b'\xfa'+pack('>f',value),deterministic=True)==value
