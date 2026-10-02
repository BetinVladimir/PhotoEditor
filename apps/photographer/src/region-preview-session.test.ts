import {describe,it,expect,vi} from 'vitest';
import {RegionPreviewSession,type RegionPreviewScope} from './region-preview-session';
import type {RegionJobTicket,RegionPreviewRequest} from './region-preview';
const id='11111111-1111-4111-8111-111111111111';
const request=():RegionPreviewRequest=>({version_id:id,recipe:{artifact_id:id,sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:1024});
const scope:RegionPreviewScope={origin:'https://photo.test',user:id,tenant:id,asset:id,revision:'r1'};
const ticket:RegionJobTicket={run_id:id,workflow_id:id,state:'queued'};
function deferred(){let resolve!:(v:RegionJobTicket)=>void;let reject!:(e:Error)=>void;const promise=new Promise<RegionJobTicket>((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};}
describe('RegionPreviewSession',()=>{
 it('ignores late replies when transport ignores abort',async()=>{
  const first=deferred(),second=deferred();const submit=vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const session=new RegionPreviewSession(submit);session.bind(scope);const a=session.request(request(),'first');const b=session.request(request(),'second');expect(submit.mock.calls[0][3].aborted).toBe(true);
  second.resolve(ticket);expect(await b).toEqual(ticket);first.resolve({...ticket,run_id:'22222222-2222-4222-8222-222222222222'});expect(await a).toBeUndefined();expect(session.currentTicket()).toEqual(ticket);
 });
 for(const field of ['origin','user','tenant','asset','revision'] as const)it(`invalidates on ${field} change`,async()=>{
  const pending=deferred();const submit=vi.fn().mockReturnValue(pending.promise);const session=new RegionPreviewSession(submit);session.bind(scope);const result=session.request(request(),'retry');session.bind({...scope,[field]:'changed'});pending.resolve(ticket);expect(await result).toBeUndefined();expect(session.currentTicket()).toBeUndefined();expect(submit.mock.calls[0][3].aborted).toBe(true);
 });
 it('preserves active errors and discards failures after logout',async()=>{
  const pending=deferred();const session=new RegionPreviewSession(()=>pending.promise);session.bind(scope);const result=session.request(request(),'retry');session.clear();pending.reject(new Error('network'));expect(await result).toBeUndefined();session.bind(scope);await expect(session.request(request(),'retry')).rejects.toThrow('network');
 });
 it('copies scope, request and ticket',async()=>{
  const pending=deferred();const submit=vi.fn().mockReturnValue(pending.promise);const session=new RegionPreviewSession(submit);const binding={...scope};session.bind(binding);binding.asset='changed';const body=request();const result=session.request(body,'retry');body.rect.x1=0.5;expect(submit.mock.calls[0][0]).toBe(id);expect(submit.mock.calls[0][1].rect.x1).toBe(1);pending.resolve(ticket);const received=await result;received!.run_id='changed';expect(session.currentTicket()).toEqual(ticket);
 });
});

it('drops derivative metadata loaded after the selection changes',async()=>{
 const session=new RegionPreviewSession(async()=>ticket);session.bind(scope);await session.request(request(),'retry');
 let complete!:(value:string)=>void;let signal!:AbortSignal;
 const loading=session.loadCurrent((_ticket,currentSignal)=>{signal=currentSignal;return new Promise<string>(resolve=>{complete=resolve;});});
 session.bind({...scope,revision:'next'});expect(signal.aborted).toBe(true);complete('old derivative');expect(await loading).toBeUndefined();
});
