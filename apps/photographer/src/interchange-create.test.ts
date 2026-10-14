import {expect,it,vi} from 'vitest';
import {freezeInterchangeCreate,readInterchangeCreateTicket,type InterchangeCreateRequest} from './interchange-create';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const imported=()=>({project_id:id(2),direction:'import' as const,format:'photoeditor_bundle' as const,bundle:{artifact_id:id(3),sha256:'a'.repeat(64)}});
const exported=()=>({project_id:id(2),direction:'export' as const,format:'photoeditor_bundle' as const,snapshot_id:id(3)});
const ticket={run_id:id(4),workflow_id:id(5),state:'queued'};
it('owns import source and preserves explicit export format',()=>{
 const source=imported(),owned=freezeInterchangeCreate(source,'original');source.bundle.sha256='b'.repeat(64);
 expect(owned).toEqual(imported());
 for(const format of ['photoeditor_bundle','xmp','captureone_sidecar'] as const)expect(freezeInterchangeCreate({...exported(),format},'original')).toEqual({...exported(),format});
});
it.each([
 {...imported(),snapshot_id:id(9)}, {...exported(),bundle:imported().bundle},
 {...imported(),bundle:null},{...exported(),snapshot_id:null},
 {...imported(),bundle:{...imported().bundle,unknown:true}},
 {...imported(),bundle:{...imported().bundle,sha256:'A'.repeat(64)}},
 {...exported(),project_id:'invalid'},{...exported(),format:'psd'},
 {...exported(),direction:'both'},
])('refuses incompatible or malformed union %#',value=>{expect(()=>freezeInterchangeCreate(value,'original')).toThrow('INTERCHANGE_CREATE_INVALID');});
it.each(['','bad key','line\nbreak','x'.repeat(129)])('rejects invalid key %#',key=>{expect(()=>freezeInterchangeCreate(exported(),key)).toThrow();});
it.each([{}, {...ticket,state:'running'},{...ticket,workflow_id:'invalid'},{...ticket,extra:true}])('rejects malformed ticket %#',value=>{expect(()=>readInterchangeCreateTicket(value)).toThrow();});
it.each([imported(),exported()])('posts exact frozen request and returns owned ticket',async body=>{
 const request=vi.fn().mockResolvedValue(ticket),api=new OwnerAPI({request} as any,id(1));
 const result=await api.createInterchange(body,'original-key');
 expect(result).toEqual(ticket);expect(result).not.toBe(ticket);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id(1)}/interchange-jobs`,{method:'POST',body,idempotencyKey:'original-key',signal:undefined});
});
it('refuses invalid union and cancellation before any HTTP',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as any,id(1));
 await expect(api.createInterchange({...exported(),bundle:imported().bundle} as unknown as InterchangeCreateRequest,'original')).rejects.toThrow();
 const controller=new AbortController();controller.abort();
 await expect(api.createInterchange(exported(),'original',controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
it('ignores a valid late ticket after cancellation',async()=>{
 const controller=new AbortController(),request=vi.fn(async()=>{controller.abort();return ticket;});
 await expect(new OwnerAPI({request} as any,id(1)).createInterchange(exported(),'original',controller.signal)).rejects.toThrow();
});
