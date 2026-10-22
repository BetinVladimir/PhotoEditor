"""Exact output ledger scenarios; synthetic staged refs do not prove physical IO."""
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.operations.semantic.preview_removal_output_closure import RemovalOutputClosure
from test_v27_removal_preview_result_binding import fixture


def setup():
    params, display_ref, display_raw, proof_ref, proof_raw, image_ref, image_raw, source_raw = fixture()
    source = parse_json(source_raw);display = parse_json(display_raw);image = parse_json(image_raw)
    inputs = [params[key] for key in ('plan','intent','candidate','protection')]+[params['source']['source'],source['pixels']]
    if source['alpha'] is not None:inputs.append(source['alpha'])
    ledger = RemovalOutputClosure(inputs,max_output_bytes=1048576)
    rows = [('pixels',image['pixels'],100,None),
        ('working_image',image_ref,len(image_raw),image_raw),
        ('preview_evidence',proof_ref,len(proof_raw),proof_raw),
        *[(role,display[role],100,None) for role in ('before','after','affected','protected')],
        ('preview_presentation',display_ref,len(display_raw),display_raw)]
    return ledger, rows, params, source_raw, display_ref


@pytest.mark.parametrize('failure',[None,'missing','unknown','duplicate','alias','bytes','root','input_sha'])
def test_output_ledger_requires_exact_reachable_set(failure):
    ledger, rows, params, source_raw, root = setup()
    if failure == 'missing':rows.pop(3)
    if failure == 'input_sha':ledger._inputs[ArtifactRef.model_validate_json(canonical_bytes(params['plan'])).artifact_id] = '0'*64
    total = sum(row[2] for row in rows)
    def run():
        for role,ref,size,document in rows:ledger.record(role,ref,size,document=document)
        if failure == 'unknown':ledger.record('extra',ArtifactRef(artifact_id=uuid4(),sha256='a'*64),1)
        if failure == 'duplicate':ledger.record('pixels',rows[0][1],100)
        if failure == 'alias':
            ledger.record('pixels',params['plan'],100)
        result = ledger.verify(params,source_raw,presentation_ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64) if failure == 'root' else root,
            expected_bytes=total+1 if failure == 'bytes' else total)
        assert result == total
    if failure:
        with pytest.raises(ValueError):run()
    else:run()


