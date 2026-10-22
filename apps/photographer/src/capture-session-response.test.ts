import {expect,it,vi} from 'vitest';
import {readShootSession,readShootSessionPage} from './capture-session-response';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref={artifact_id:id(5),sha256:'a'.repeat(64)};
const policy=()=>({schema_version:1,time_gap_seconds:30,neighbor_count:2,protect_manual_picks:true,camera_offsets:[],calibration:{...ref}});
function wire(){const data={resource_type:'shoot_sessions',id:id(1),tenant_id:id(2),project_id:id(3),collection_id:id(4),name:'Studio',revision:'9223372036854775807',consent_epoch:'1',created_at:'2026-10-17T10:00:00Z',updated_at:'2026-10-17T10:00:00Z',state:'open',policy:policy(),capture_policy:null};return {id:data.id,revision:data.revision,document:null,data};}
it('accepts actual nullable legacy capture policy in every readonly state and detaches nested policy',()=>{
 for(const state of ['open','closed','archived']){const raw=wire();raw.data.state=state;const value=readShootSession(raw,id(2),{id:id(1),project:id(3),collection:id(4)});expect(value.capture_policy).toBeNull();raw.data.policy.calibration.sha256='b'.repeat(64);expect(value.policy.calibration.sha256).toBe('a'.repeat(64));}
});
it('uses actual SQL 200 unicode codepoint boundary, not code units or declared old256',()=>{const raw=wire();raw.data.name='📷'.repeat(200);expect(readShootSession(raw,id(2)).name).toHaveLength(400);raw.data.name+='📷';expect(()=>readShootSession(raw,id(2))).toThrow();});
it('refuses cross scope, UUID/revision/envelope drift and malformed actual policies',()=>{
 for(const patch of [{tenant_id:id(8)},{project_id:'wrong'},{collection_id:null},{revision:'9223372036854775808'},{consent_epoch:'0'},{policy:{}},{policy:{...policy(),protect_manual_picks:false}},{capture_policy:{}},{capture_policy:{schema_version:1,checks:['focus','focus'],max_queue_age_seconds:30,calibration:ref}}]){const raw=wire();Object.assign(raw.data,patch);raw.revision=raw.data.revision;expect(()=>readShootSession(raw,id(2))).toThrow();}
 expect(()=>readShootSession(wire(),id(2),{id:id(8)})).toThrow();expect(()=>readShootSession(wire(),id(2),{project:id(8)})).toThrow();expect(()=>readShootSession(wire(),id(2),{collection:id(8)})).toThrow();const bad=wire();bad.id=id(8);expect(()=>readShootSession(bad,id(2))).toThrow();
});
it('accepts enabled capture assistance and exact project pages while refusing malformed pagination',()=>{
 const raw=wire();Object.assign(raw.data,{capture_policy:{schema_version:1,checks:['focus'],max_queue_age_seconds:30,calibration:ref}});expect(readShootSession(raw,id(2)).capture_policy?.checks).toEqual(['focus']);
 expect(readShootSessionPage({items:[raw],next_cursor:null},id(2),id(3)).items).toHaveLength(1);
 for(const page of [{items:[raw,raw],next_cursor:null},{items:[raw],next_cursor:'same'},{items:[],next_cursor:'next'},{items:[raw],next_cursor:[]}])expect(()=>readShootSessionPage(page,id(2),id(3),'same')).toThrow();expect(()=>readShootSessionPage({items:[raw],next_cursor:null},id(2),id(8))).toThrow();
});
it('mounts exact session and page decoders before readonly producer consumers with pre and late abort',async()=>{
 const request=vi.fn().mockResolvedValue(wire()),api=new OwnerAPI({request} as never,id(2));expect((await api.shootSession(id(1))).project_id).toBe(id(3));const foreign=wire();foreign.data.tenant_id=id(8);request.mockResolvedValue(foreign);await expect(api.shootSession(id(1))).rejects.toThrow();
 request.mockResolvedValue({items:[wire()],next_cursor:null});expect((await api.shootSessions(id(3))).items[0].collection_id).toBe(id(4));
 let resolve!:(v:unknown)=>void;request.mockImplementation(()=>new Promise(done=>{resolve=done}));const controller=new AbortController(),pending=api.shootSession(id(1),controller.signal);controller.abort();resolve(wire());await expect(pending).rejects.toMatchObject({name:'AbortError'});const count=request.mock.calls.length;await expect(api.shootSession(id(1),controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(count);
});
it('preserves exact accepted CAS replay and refuses a different revision or policy without changing its caller key',async()=>{
 const raw=wire();raw.revision=raw.data.revision='2';raw.data.state='closed';
 const request=vi.fn().mockResolvedValue(raw),api=new OwnerAPI({request} as never,id(2)),body={expected_revision:'1',name:'Studio',state:'closed' as const,policy:policy() as import('./capture-types').ShootSessionPolicy,capture_policy:null};
 await api.updateShootSession(id(1),body,'same-cas-key');await api.updateShootSession(id(1),body,'same-cas-key');expect(request.mock.calls.every(call=>call[1].idempotencyKey==='same-cas-key')).toBe(true);
 const wrong=wire();wrong.revision=wrong.data.revision='3';wrong.data.state='closed';request.mockResolvedValue(wrong);await expect(api.updateShootSession(id(1),body,'same-cas-key')).rejects.toThrow('CAS_RECEIPT');
 request.mockResolvedValue({...raw,data:{...raw.data,policy:{...policy(),neighbor_count:3}}});await expect(api.updateShootSession(id(1),body,'same-cas-key')).rejects.toThrow('CAS_RECEIPT');expect(body.expected_revision).toBe('1');
});
