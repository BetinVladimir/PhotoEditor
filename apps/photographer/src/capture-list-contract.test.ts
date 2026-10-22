import {expect,it} from 'vitest';
import {readCaptureCollections,readCaptureSessions} from './capture-list-contract';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const session={id:id(3),tenant_id:id(1),project_id:id(2),name:'Studio',state:'open',revision:'1',policy:{},capture_policy:null};
const collection={id:id(4),tenant_id:id(1),project_id:id(2),name:'Frames',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'};
it('validates own project/tenant session projection, exact scalar states/revisions and duplicate/cursor bounds',()=>{
 const decoded=readCaptureSessions({items:[session],next_cursor:'next'},id(1),id(2));expect(decoded.items[0]).toEqual(session);expect(decoded.items[0]).not.toBe(session);
 for(const patch of [{tenant_id:id(8)},{project_id:id(8)},{id:'forged'},{name:[]},{state:['open']},{revision:'9223372036854775808'}])expect(()=>readCaptureSessions({items:[{...session,...patch}],next_cursor:null},id(1),id(2))).toThrow();
 expect(()=>readCaptureSessions({items:[session,session]},id(1),id(2))).toThrow();expect(()=>readCaptureSessions({items:[session],next_cursor:'x'.repeat(2049)},id(1),id(2))).toThrow();
});
it('validates the actual collection DTO before selecting fresh writable destinations',()=>{
 expect(readCaptureCollections({items:[collection,{...collection,id:id(5),state:'archived'},{...collection,id:id(6),state:'deleting'}]},id(1),id(2)).items).toEqual([collection]);
 expect(()=>readCaptureCollections({items:[{id:id(4),name:'incomplete'}]},id(1),id(2))).toThrow();expect(()=>readCaptureCollections({items:[{...collection,project_id:id(8)}]},id(1),id(2))).toThrow();
});
