"""Canonical legacy restore probe document fixture for acceptance reader tests."""
from hashlib import sha256
from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes


def legacy_probe_observations(replay):
    side={'restore_run_id':str(replay.run_id),'database':replay.database,
        'build_sha256':replay.build_sha256,'recovery_sha256':replay.recovery_sha256,
        'replay_sha256':replay.replay_sha256}
    legacy={'schema_version':'restore-legacy-fixture/v1',
        'preview':dict(side),'apply':dict(side)}
    template={'format':'jpeg','icc_id':'srgb_v1','icc_sha256':'1'*64,
        'quality':90,'alpha':'matte','matte_rgb':[1,1,1],
        'metadata_policy':'strip','subsampling':'444'}
    fixture={'schema_version':'restore-legacy-complete-fixture/v1','legacy':legacy,
        'preset_id':str(uuid4()),'preset_revision':1,'preset_sha256':'2'*64,
        'selection_key_sha256':'3'*64,'export_key_sha256':'4'*64,
        'intent_key_sha256':'5'*64,'encode_params_template':template,
        'encode_params_template_sha256':sha256(canonical_bytes(template)).hexdigest(),
        'scratch_root':'/private/tmp/restore','buckets':['photos'],
        'export_maximum_seconds':30,'maximum_seconds':300,'pepper_sha256':'6'*64}
    apply_run,output,applied,selection,encoded,volume=(uuid4() for _ in range(6))
    params={'image':{'artifact_id':str(applied),'sha256':'7'*64},**template}
    dynamic={'schema_version':'restore-selection-export-fixture/v1',
        'apply_fixture':legacy['apply'],'preset_id':fixture['preset_id'],
        'request_key_sha256':fixture['export_key_sha256'],
        'intent_key_sha256':fixture['intent_key_sha256'],
        'selection_key_sha256':fixture['selection_key_sha256'],
        'scratch_root':fixture['scratch_root'],
        'encode_params_sha256':sha256(canonical_bytes(params)).hexdigest(),
        'selection':{'snapshot_digest':'8'*64,'preset_revision':1,'preset_sha256':'2'*64},
        'apply_ticket':{'run_id':str(apply_run),'output_collection_id':str(output)},
        'selection_collection_id':str(output)}
    observations={'fixture':fixture,'dynamic_selection_export':dynamic,
        'dynamic_selection_export_sha256':sha256(canonical_bytes(dynamic)).hexdigest(),
        'apply_run_id':str(apply_run),'apply_output_collection_id':str(output),
        'apply_document_artifact_id':str(applied),'selection_snapshot_id':str(selection),
        'selection_digest':'8'*64,'encoded_artifact_id':str(encoded),
        'encoded_sha256':'9'*64,'volume_artifact_id':str(encoded),
        'volume_sha256':'9'*64,'download_sha256':'9'*64,
        'delivery_kind':'single','archive_sha256':None,
        'preview_sha256':'a'*64,'apply_document_sha256':'7'*64,
        'preview_polls':1,'apply_polls':1,'export_polls':1}
    return sha256(canonical_bytes(fixture)).hexdigest(),observations
