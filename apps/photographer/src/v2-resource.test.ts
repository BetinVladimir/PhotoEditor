import {describe,it,expect} from 'vitest';
import {v2ResourceData} from './v2-resource';
const row={resource_type:'shoot_sessions',id:'session',revision:'9007199254740993',name:'Studio',project_id:'project',capture_policy:{checks:['focus']}};
const envelope={id:row.id,revision:row.revision,document:null,data:row};
describe('V2 resource envelope bindings',()=>{
 it('returns the row while retaining decimal revisions and policy fields',()=>{
  expect(v2ResourceData(envelope,'shoot_sessions','session')).toEqual(row);
 });
 it('rejects flat rows, mismatched identities, kinds and revisions',()=>{
  for(const value of [row,{...envelope,id:'other'},{...envelope,revision:'2'},{...envelope,data:{...row,resource_type:'project_persons'}},{...envelope,data:null}])expect(()=>v2ResourceData(value,'shoot_sessions','session')).toThrow();
 });
});
