"""Authored source/lifecycle authority scenarios; deliberately not executed."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


def test_download_source_failure_private_helpers_and_bound_inventory(db):
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.fail_download_output_locked(uuid,uuid,integer[])','EXECUTE')").fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.download_export_bound_input(uuid,uuid,bigint,uuid,text,uuid,text,text)','EXECUTE')").fetchone()[0] is True
    assert db.execute("SELECT * FROM photo.download_export_bound_input(%s,%s,0,%s,%s,%s,%s,'working_rgb')",(uuid4(),uuid4(),uuid4(),'a'*64,uuid4(),'b'*64)).fetchall()==[]
    assert db.execute("SELECT photo.fail_download_planning_source(%s,%s,0,%s,%s,'ARTIFACT_MISSING')",(uuid4(),uuid4(),uuid4(),'a'*64)).fetchone()[0] is False


@pytest.mark.parametrize('change',[{'watermark':{}},{'sharpen':{}},{'metadata_policy':'copyright'},{'format':'avif'},{'icc_id':'display_p3_v1'},{'size':{'fit':'cover'}}])
def test_legacy_download_refuses_presentation_presets_before_acceptance(db,change):
    preset={'format':'jpeg','icc_id':'srgb_v1','subsampling':'444','alpha':'matte','metadata_policy':'strip'}
    assert db.execute("SELECT photo.download_preset_compatible(%s)",(Jsonb(preset),)).fetchone()[0] is True
    preset.update(change)
    assert db.execute("SELECT photo.download_preset_compatible(%s)",(Jsonb(preset),)).fetchone()[0] is False
