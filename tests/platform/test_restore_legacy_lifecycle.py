from types import SimpleNamespace as NS
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_legacy_lifecycle as m


@pytest.mark.parametrize('mode',['valid','pin','apply_preflight','preview_failure','apply_failure','late_preview','late_apply','inactive','fixture_denied'])
def test_combined_legacy_never_accepts_partial_or_late_changed_results(monkeypatch,mode):
    descriptor={'preview':{'pin':'preview'},'apply':{'pin':'apply'}}
    monkeypatch.setattr(m,'legacy_fixture_descriptor',lambda *a:descriptor)
    def preflight(*a,**k):
        if mode=='apply_preflight':raise ValueError('invalid Apply installation')
    monkeypatch.setattr(m,'validate_fixture_legacy_apply',preflight)
    events=[];preview=NS(ticket='preview-ticket',result='preview-result');applied=NS(ticket='apply-ticket',result='apply-result')
    def run_preview(*a,**k):
        events.append('preview')
        if mode=='preview_failure':raise ValueError('preview failed')
        return preview
    def run_apply(*a,**k):
        events.append('apply')
        if mode=='apply_failure':raise ValueError('apply failed')
        return applied
    monkeypatch.setattr(m,'run_pinned_preview',run_preview)
    monkeypatch.setattr(m,'run_pinned_legacy_apply',run_apply)
    monkeypatch.setattr(m,'observe_fixture_legacy_preview_result',lambda *a,**k:'changed' if mode=='late_preview' else preview.result)
    monkeypatch.setattr(m,'observe_fixture_legacy_apply_result',lambda *a,**k:'changed' if mode=='late_apply' else applied.result)
    installation=m.RestoreLegacyInstallation(NS(),NS(),NS(),NS(),32,16,30)
    pin='f'*64 if mode=='pin' else sha256(canonical_bytes(descriptor)).hexdigest()
    def invoke():return m.run_pinned_legacy_observations(NS(database='restore'),pin,None,None,[],installation,
        srgb_profile=b'x'*128,icc_registry=None,tls_context=None,
        assert_installed_fixture=lambda:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().apply is applied and events==['preview','apply']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode in ('pin','apply_preflight','inactive','fixture_denied'):assert not events
    assert events.count('preview')<=1 and events.count('apply')<=1
