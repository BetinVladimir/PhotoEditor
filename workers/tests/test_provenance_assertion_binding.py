"""Physical assertion hash/inventory join; no SDK or COSE qualification."""
from dataclasses import replace
from hashlib import sha256,sha384,sha512

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningLineage
from photo_worker.operations.provenance.assertion_binding import verify_assertion_bindings
from photo_worker.operations.provenance.jumbf_claim import ActiveClaimBytes,AssertionBytes,type_uuid


def fixture(algorithm='sha256',version=2):
    lineage=SigningLineage(schema_version=1,source_hashes=['a'*64],actions=[])
    raw=canonical_bytes(lineage)
    assertion=AssertionBytes('org.photoeditor.lineage',type_uuid(b'json'),b'original-jumd-and-content',((b'json',raw),))
    extracted=ActiveClaimBytes('active','c2pa.claim.v2' if version==2 else 'c2pa.claim',b'claim',b'sig',(assertion,))
    digest={'sha256':sha256,'sha384':sha384,'sha512':sha512}[algorithm](assertion.contents).digest()
    claim={'alg':algorithm,'created_assertions' if version==2 else 'assertions':[
        {'url':'self#jumbf=c2pa.assertions/org.photoeditor.lineage','hash':digest}]}
    return extracted,claim,lineage


@pytest.mark.parametrize('version',[1,2])
@pytest.mark.parametrize('algorithm',['sha256','sha384','sha512'])
def test_exact_original_assertion_hash_and_lineage(version,algorithm):
    verify_assertion_bindings(*fixture(algorithm,version))


@pytest.mark.parametrize('mode',['bytes','hash','uri','duplicate','undeclared','payload','type','lineage','missing','version'])
def test_missing_ambiguous_or_changed_assertions_refused(mode):
    extracted,claim,lineage=fixture()
    reference=claim['created_assertions'][0]
    assertion=extracted.assertions[0]
    if mode=='bytes':extracted=replace(extracted,assertions=(replace(assertion,contents=b'changed'),))
    if mode=='hash':reference['hash']=b'wrong'
    if mode=='uri':reference['url']='self#jumbf=/c2pa/other/c2pa.assertions/org.photoeditor.lineage'
    if mode=='duplicate':claim['created_assertions'].append(dict(reference))
    if mode=='undeclared':extracted=replace(extracted,assertions=extracted.assertions+(replace(assertion,label='extra'),))
    if mode=='payload':extracted=replace(extracted,assertions=(replace(assertion,payloads=assertion.payloads*2),))
    if mode=='type':extracted=replace(extracted,assertions=(replace(assertion,type_uuid=type_uuid(b'cbor')),))
    if mode=='lineage':lineage=lineage.model_copy(update={'source_hashes':['b'*64]})
    if mode=='missing':extracted=replace(extracted,assertions=())
    if mode=='version':claim['assertions']=claim['created_assertions']
    with pytest.raises(ValueError):verify_assertion_bindings(extracted,claim,lineage)


def test_absolute_uri_and_per_reference_hash_algorithm():
    extracted,claim,lineage=fixture()
    reference=claim['created_assertions'][0]
    reference['url']='self#jumbf=/c2pa/active/c2pa.assertions/org.photoeditor.lineage'
    reference['alg']='sha384'
    reference['hash']=sha384(extracted.assertions[0].contents).digest()
    verify_assertion_bindings(extracted,claim,lineage)


def test_cancellation_propagates():
    def active():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):verify_assertion_bindings(*fixture(),check_active=active)


def test_boolean_denial_refuses_assertion_before_and_after_binding():
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_assertion_bindings(*fixture(),check_active=lambda:False)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        return calls<3
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_assertion_bindings(*fixture(),check_active=active)
