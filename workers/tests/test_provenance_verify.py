from copy import deepcopy
import pytest
from photo_worker.operations.provenance.verify import verify_sdk_reader


class Reader:
    def __init__(self):
        self.embedded, self.state = True, 'Trusted'
        self.results = {'activeManifest': {'failure': [], 'informational': [],
            'success': [{'code': code} for code in (
                'claimSignature.validated', 'claimSignature.insideValidity',
                'signingCredential.trusted', 'assertion.dataHash.match',
                'timeStamp.validated', 'timeStamp.trusted',
                'signingCredential.ocsp.notRevoked')]}}
        self.manifest = {'format': 'image/png', 'assertions': [
            {'label': 'org.photoeditor.lineage', 'data': {'schema_version': 1}}]}

    def is_embedded(self):
        return self.embedded

    def get_validation_state(self):
        return self.state

    def get_validation_results(self):
        return self.results

    def get_active_manifest(self):
        return self.manifest


@pytest.mark.parametrize('mode', ['valid', 'remote', 'untrusted', 'signature',
    'binding', 'timestamp', 'ocsp', 'active_failure', 'ingredient_failure',
    'extra_exclusion', 'success_exclusion', 'mime', 'lineage', 'duplicate', 'missing'])
def test_sdk_validation_report_requires_explicit_verified_statuses(mode):
    reader = Reader()
    remove = {'signature': 'claimSignature.validated', 'binding': 'assertion.dataHash.match',
              'timestamp': 'timeStamp.trusted', 'ocsp': 'signingCredential.ocsp.notRevoked'}
    if mode in remove:
        reader.results['activeManifest']['success'] = [
            status for status in reader.results['activeManifest']['success']
            if status['code'] != remove[mode]]
    elif mode == 'remote':
        reader.embedded = False
    elif mode == 'untrusted':
        reader.state = 'Valid'
    elif mode == 'active_failure':
        reader.results['activeManifest']['failure'] = [{'code': 'assertion.dataHash.mismatch'}]
    elif mode == 'ingredient_failure':
        reader.results['ingredientDeltas'] = [{'validationDeltas': {'failure': [{'code': 'bad'}]}}]
    elif mode == 'extra_exclusion':
        reader.results['activeManifest']['informational'] = [{'code': 'assertion.boxesHash.additionalExclusionsPresent'}]
    elif mode == 'success_exclusion':
        reader.results['activeManifest']['success'].append(
            {'code': 'assertion.boxesHash.additionalExclusionsPresent'})
    elif mode == 'mime':
        reader.manifest['format'] = 'image/jpeg'
    elif mode == 'lineage':
        reader.manifest['assertions'][0]['data'] = {'schema_version': 2}
    elif mode == 'duplicate':
        reader.manifest['assertions'].append(deepcopy(reader.manifest['assertions'][0]))
    elif mode == 'missing':
        reader.results = None
    invoke = lambda: verify_sdk_reader(reader, {'timestamp_mode': 'required',
                        'revocation_mode': 'required'}, {'schema_version': 1}, 'image/png')
    if mode == 'valid':
        assert len(invoke()) == 64
    else:
        with pytest.raises(ValueError):
            invoke()



def test_active_manifest_has_independent_size_limit():
    reader=Reader()
    reader.manifest['oversized']='x'*(8<<20)
    with pytest.raises(ValueError,match='SIGNER_VALIDATION_LIMIT'):
        verify_sdk_reader(reader,{'timestamp_mode':'required','revocation_mode':'required'},
                          {'schema_version':1},'image/png')


@pytest.mark.parametrize('ingredients', [None, 'ignored', 7,
    {'validationDeltas': {'failure': None}},
    {'validationDeltas': {'failure': 'ignored'}}])
def test_sdk_report_rejects_malformed_ingredient_failures(ingredients):
    reader=Reader()
    reader.results['ingredientDeltas']=ingredients
    with pytest.raises(ValueError,match='SIGNER_INGREDIENT_SHAPE'):
        verify_sdk_reader(reader,{'timestamp_mode':'required','revocation_mode':'required'},
                          {'schema_version':1},'image/png')


@pytest.mark.parametrize('location', ['results', 'manifest'])
def test_sdk_report_rejects_deep_tree_before_json_serialization(location):
    reader = Reader()
    nested = []
    current = nested
    for _ in range(40):
        child = []
        current.append(child)
        current = child
    if location == 'results':
        reader.results['nested'] = nested
    else:
        reader.manifest['nested'] = nested
    with pytest.raises(ValueError, match='SIGNER_VALIDATION_LIMIT'):
        verify_sdk_reader(reader, {'timestamp_mode':'required', 'revocation_mode':'required'},
                          {'schema_version':1}, 'image/png')


@pytest.mark.parametrize('method',['get_validation_results','get_active_manifest'])
def test_cancelled_native_reader_result_is_never_accepted(method):
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    reader=Reader();live=True;original=getattr(reader,method)
    def active():
        if not live:raise RuntimeError('cancelled')
    def cancel_on_read():
        nonlocal live
        live=False
        return original()
    setattr(reader,method,cancel_on_read)
    with signing_task_scope(active):
        with pytest.raises(RuntimeError,match='cancelled'):
            verify_sdk_reader(reader,{'timestamp_mode':'required','revocation_mode':'required'},
                              {'schema_version':1},'image/png')
        live=True


def test_explicit_boolean_denial_prevents_reader_and_refuses_late_result():
    reader=Reader();calls=[]
    original=reader.get_validation_results
    def results():
        calls.append('results')
        return original()
    reader.get_validation_results=results
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_sdk_reader(reader,{'timestamp_mode':'required','revocation_mode':'required'},
            {'schema_version':1},'image/png',check_active=lambda:False)
    assert not calls
    live=True
    def cancel_on_results():
        nonlocal live
        calls.append('results')
        value=original();live=False
        return value
    reader.get_validation_results=cancel_on_results
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_sdk_reader(reader,{'timestamp_mode':'required','revocation_mode':'required'},
            {'schema_version':1},'image/png',check_active=lambda:live)
    assert calls==['results']
