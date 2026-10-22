"""Bounded series graph registry/ACL regression. Authored, not executed."""
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def test_series_packs_register_without_changing_balance_authority(db):
    tenant,run,token,_=_build(db)
    plan=[dict(phase=p,operation=o,output_role=r,cardinality=c) for p,o,r,c in [
        ('resolve','style.resolve','resolved_style','item'),('color','image.color','working_image','item'),('series_measure','series.measure','series_stats','item'),('series_pack','series.pack','series_stats_pack','group'),('series_balance','series.balance','series_corrections','aggregate'),('local','image.local','working_image','item'),('quality','quality.check','quality_report','item')]]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    assert db.execute("SELECT resource_class,routing_key,backends FROM photo.operation_registry WHERE operation='series.pack' AND version='1'").fetchone()==('cpu','cpu.series_pack',['cpu'])
    assert db.execute("SELECT max_bytes,max_count FROM photo.operation_output_policies WHERE operation='series.pack' AND role='series_stats_pack'").fetchone()==(1048576,1)
    assert db.execute("SELECT max_bytes FROM photo.operation_output_policies WHERE operation='series.balance' AND role='series_corrections'").fetchone()==(8388608,)
    assert db.execute("SELECT timeout_seconds FROM photo.operation_registry WHERE operation='series.balance' AND version='1'").fetchone()==(7200,)
