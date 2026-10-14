"""Actual media encoder and adoption verifier over version-pinned MinIO bytes.

Catalog scope is controlled here; live SQL receipts are a separate gate.
"""
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import importlib.util

import pytest

from conftest import ROOT
from test_v25_lens_objectstore import lens_objects
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_derivative_store import PostgresRenderDerivativeReader
from photo_worker.v2.render_derivative_adoption import verify_render_derivative_result


@pytest.mark.parametrize('transparent', [False, True])
def test_real_encoded_adoption_reads_exact_versions(lens_objects, tmp_path, monkeypatch, transparent):
    client, bucket = lens_objects
    spec = importlib.util.spec_from_file_location('real_render_encoder_case',
        ROOT/'tests/platform/test_render_result_preview.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    def adopt(case):
        tenant, run, task, result = [uuid4() for _ in range(4)]
        versions = {}
        by_id = {ref.artifact_id: ref for ref in case['bodies']}
        for ref, body in case['bodies'].items():
            mime = case['metadata'][ref][2]
            key = str(ref.artifact_id)
            original = client.put_object(bucket, key, BytesIO(body), len(body),
                                         content_type=mime).version_id
            wrong = b'wrong newer version'
            replacement = client.put_object(bucket, key, BytesIO(wrong), len(wrong),
                                            content_type=mime).version_id
            assert original and original != replacement
            versions[ref] = original

        def query(sql, args):
            assert args[:3] == (tenant, run, 'preview1600')
            if 'v26_render_derivative_result(' in sql:
                rows = [(task, result, 1, 1, ref.artifact_id, ref.sha256)
                        for ref in (case['manifest'], case['derivative'])]
            else:
                assert args[3:6] == (result, 1, 1)
                ref = by_id[args[6]]
                source, role, mime = case['metadata'][ref]
                rows = [(bucket, str(ref.artifact_id), versions[ref], ref.sha256,
                         len(case['bodies'][ref]), mime, role, source)]
            return SimpleNamespace(fetchall=lambda: rows)

        def reader():
            return PostgresRenderDerivativeReader(
                SimpleNamespace(autocommit=True, execute=query), client,
                tenant_id=tenant, run_id=run, task_id=task, result_id=result,
                variant='preview1600', attempt=1, fence=1, allowed_bucket=bucket)

        def verify():
            return verify_render_derivative_result(reader(), case['params'],
                case['manifest'], target_icc_sha256=case['target_sha'],
                working_icc_sha256=case['working_sha'])

        assert verify() == case['document']
        # Even matching length cannot substitute for the pinned digest/codec.
        ref = case['derivative']
        wrong = bytes(len(case['bodies'][ref]))
        versions[ref] = client.put_object(bucket, str(ref.artifact_id),
            BytesIO(wrong), len(wrong), content_type=case['metadata'][ref][2]).version_id
        with pytest.raises(AdoptionInvalid, match="RENDER_DERIVATIVE_ENCODING_INVALID"):
            verify()

    module.test_render_image_result_through_real_media_encoder(
        tmp_path, transparent, monkeypatch, adoption_case=adopt)
