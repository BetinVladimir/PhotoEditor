"""Controlled DB transport tests, not installed SQL or qualification evidence."""
from pathlib import Path
import pytest
from test_authored_render_live_scope import fixture
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2 import authored_render_scope_inventory as m

def inventory():return dict(context={},base_receipt={},base_receipt_jcs='{}',phases=[])

@pytest.mark.parametrize('kind',['final','segment','geometry'])
def test_exact_reader_and_lease_tuple_between_full_gates(kind):
 raw,ctx,db,calls=fixture(kind);original=db._query;seen=[];value=inventory()
 if kind=='geometry':value.update(parallel_source={},parallel_source_jcs='{}',source_jcs='{}')
 def query(sql,args):
  if 'SELECT photo.read_' in sql:seen.append((sql,args));return (value,)
  return original(sql,args)
 db._query=query
 assert m.SQLAuthoredScopeInventory(db).read(ctx,raw) is value
 assert len(calls)==4 and len(seen)==1
 assert m.READERS[(ctx.lease.command.operation,ctx.lease.command.operation_version)] in seen[0][0]
 assert seen[0][1]==calls[0][1]

@pytest.mark.parametrize('value',[None,(),(None,),({},),(True,), (inventory(),inventory())])
def test_unavailable_or_open_shape_refused(value):
 raw,ctx,db,calls=fixture();original=db._query
 db._query=lambda sql,args:value if 'SELECT photo.read_' in sql else original(sql,args)
 with pytest.raises(OperationFailure):m.SQLAuthoredScopeInventory(db).read(ctx,raw)

def test_late_revocation_does_not_release_inventory():
 raw,ctx,db,calls=fixture();original=db._query;read=[]
 def query(sql,args):
  if 'SELECT photo.read_' in sql:read.append(True);return (inventory(),)
  if read:return (False,)
  return original(sql,args)
 db._query=query
 with pytest.raises(OperationFailure):m.SQLAuthoredScopeInventory(db).read(ctx,raw)
 assert read
