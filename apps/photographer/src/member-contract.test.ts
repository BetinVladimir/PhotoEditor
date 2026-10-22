import {describe,it,expect,vi} from 'vitest';
import {readMember,readMemberPage} from './member-contract';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(1),user_id:id(2),email:'member@example.test',role:'editor',status:'active',revision:'9223372036854775807'});
describe('membership wire contract',()=>{
 it('keeps int64 revision exact and owns the returned projection',()=>{const input=row(),decoded=readMember(input);expect(decoded.revision).toBe('9223372036854775807');input.email='changed';expect(decoded.email).toBe('member@example.test')});
 it('does not confuse membership identity with mutation user identity',()=>{expect(()=>readMember({...row(),user_id:id(3)},row())).toThrow();expect(()=>readMember({...row(),id:id(3)},row())).toThrow()});
 it('rejects malformed/duplicated members and continuing empty pages',()=>{
  for(const changed of [{user_id:'user'},{id:'00000000-0000-0000-0000-000000000000'},{role:'superuser'},{status:'deleted'},{revision:'9223372036854775808'},{revision:1},{extra:true}])expect(()=>readMember({...row(),...changed})).toThrow();
  for(const page of [{items:[row(),row()],next_cursor:null},{items:[],next_cursor:'next'},{items:[row()],next_cursor:[]},{items:[row()],next_cursor:null,locator:'private'}])expect(()=>readMemberPage(page)).toThrow();
 });
 it('decodes actual OwnerAPI member reads and refuses invalid responses',async()=>{const request=vi.fn().mockResolvedValue({items:[row()],next_cursor:null});const api=new OwnerAPI({request} as unknown as ApiClient,id(4));expect((await api.members()).items[0].user_id).toBe(id(2));request.mockResolvedValue({items:[{...row(),user_id:'wrong'}],next_cursor:null});await expect(api.members()).rejects.toThrow()});
 it('binds member CAS to user UUID rather than membership UUID and exact full int64 increment',async()=>{
  const before={...row(),revision:'9223372036854775806'} as import('./catalog-types').Member,accepted={...before,role:'viewer',status:'suspended',revision:'9223372036854775807'};
  const request=vi.fn().mockResolvedValue(accepted),api=new OwnerAPI({request} as unknown as ApiClient,id(4));await api.updateMember(before,'viewer','suspended');
  expect(request.mock.calls[0][0]).toContain(`/members/${id(2)}`);expect(request.mock.calls[0][1]).toMatchObject({method:'PATCH',headers:{'If-Match':'"9223372036854775806"'},body:{role:'viewer',status:'suspended'}});
  for(const receipt of [{...accepted,id:id(8)},{...accepted,user_id:id(8)},{...accepted,role:'editor'},{...accepted,revision:before.revision}]){request.mockResolvedValue(receipt);await expect(api.updateMember(before,'viewer','suspended')).rejects.toThrow('MEMBER_RESPONSE_INVALID');}
 });
});
