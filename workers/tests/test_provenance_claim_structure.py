from copy import deepcopy
from types import SimpleNamespace

import pytest

from photo_worker.operations.provenance.claim_structure import verify_claim_structure


def claim(version):
    value = {'instanceID': 'xmp:iid:test', 'signature': 'self#jumbf=c2pa.signature',
             'claim_generator_info': {'name': 'PhotoEditor'}, 'created_assertions': [{}]}
    if version == 1:
        value['claim_generator_info'] = [value['claim_generator_info']]
        value['assertions'] = value.pop('created_assertions')
        value.update({'claim_generator': 'PhotoEditor/2.0', 'dc:format': 'image/png'})
    return value


@pytest.mark.parametrize('version', [1, 2])
def test_required_claim_structure(version):
    verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim' if version == 1 else 'c2pa.claim.v2'),
                           claim(version), mime='image/png')


@pytest.mark.parametrize('version', [1, 2])
@pytest.mark.parametrize('mode', ['instance', 'signature', 'generator', 'name', 'assertions', 'mixed', 'redactions'])
def test_malformed_required_fields_fail(version, mode):
    value = deepcopy(claim(version))
    if mode == 'instance': value.pop('instanceID')
    elif mode == 'signature': value['signature'] = 1
    elif mode == 'generator': value['claim_generator_info'] = [] if version == 2 else {}
    elif mode == 'name': value['claim_generator_info'] = {} if version == 2 else [{}]
    elif mode == 'assertions': value['created_assertions' if version == 2 else 'assertions'] = []
    elif mode == 'mixed': value['assertions' if version == 2 else 'gathered_assertions'] = [{}]
    elif mode == 'redactions': value['redacted_assertions'] = [None]
    with pytest.raises(ValueError):
        verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim' if version == 1 else 'c2pa.claim.v2'), value)


def test_v1_mime_must_match_reader():
    with pytest.raises(ValueError, match='SIGNER_CLAIM_MIME'):
        verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim'), claim(1), mime='image/jpeg')


def test_present_optional_list_cannot_be_empty():
    value = claim(2)
    value['gathered_assertions'] = []
    with pytest.raises(ValueError, match='SIGNER_CLAIM_ASSERTIONS'):
        verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim.v2'), value)


@pytest.mark.parametrize('version', [1, 2])
@pytest.mark.parametrize('field', ['unknown', 1, b'alg'])
def test_unknown_claim_fields_rejected(version, field):
    value = claim(version)
    value[field] = 'value'
    with pytest.raises(ValueError, match='SIGNER_CLAIM_UNKNOWN_FIELD'):
        verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim' if version == 1 else 'c2pa.claim.v2'), value)


@pytest.mark.parametrize('mode', ['unknown', 'numeric_key', 'icon', 'specVersion', 'metadata'])
def test_generator_and_metadata_envelopes(mode):
    value = claim(2)
    generator = value['claim_generator_info']
    if mode == 'unknown': generator['private_field'] = True
    elif mode == 'numeric_key': generator[1] = True
    elif mode == 'icon': generator['icon'] = 'url'
    elif mode == 'specVersion': generator['specVersion'] = 2
    elif mode == 'metadata': value['metadata'] = []
    with pytest.raises(ValueError):
        verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim.v2'), value)


def test_entity_namespaced_generator_extension_is_preserved():
    value = claim(2)
    value['claim_generator_info'].update({'org.photoeditor.build': {'revision': 'test'}, 'specVersion': '2.4.0'})
    verify_claim_structure(SimpleNamespace(claim_label='c2pa.claim.v2'), value)
