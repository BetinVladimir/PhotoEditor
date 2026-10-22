// @vitest-environment jsdom
import {afterEach,describe,it,expect,vi} from 'vitest';
import {abandonDownloadPreparation} from '../../portal/src/lib/api';
const uuid=(n:number)=>`20000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body=()=>({selection_key:uuid(1),start_key:uuid(2),selection:{collection_revision:'1',scope:'all'},preset_id:uuid(3),snapshot_id:null});
afterEach(()=>vi.unstubAllGlobals());
describe('exact Portal abandon wire resolution',()=>{
 for(const change of ['selection_key','start_key','preset_id','collection_id','extra'])it(`refuses changed ${change}`,async()=>{
  const request=body(),response:Record<string,unknown>={status:'abandoned',selection_key:request.selection_key,start_key:request.start_key,preset_id:request.preset_id,collection_id:uuid(4)};
  response[change]=change==='collection_id'?'invalid':uuid(8);vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(response),{status:200})));
  await expect(abandonDownloadPreparation('csrf',request)).rejects.toThrow();
 });
 it('freezes the request before transport and validates accepted original ticket',async()=>{
  const request=body();let finish!:(v:Response)=>void;
  const fetch=vi.fn(()=>new Promise<Response>(resolve=>{finish=resolve;}));vi.stubGlobal('fetch',fetch);
  const promise=abandonDownloadPreparation('csrf',request);request.selection_key=uuid(9);request.selection.collection_revision='99';
  finish(new Response(JSON.stringify({status:'accepted',selection_key:uuid(1),start_key:uuid(2),preset_id:uuid(3),ticket:{download_job_id:uuid(5),status:'queued'}}),{status:200}));
  expect((await promise).status).toBe('accepted');expect(JSON.parse((fetch.mock.calls as unknown as Array<[string,RequestInit]>)[0][1].body as string).selection.collection_revision).toBe('1');
 });
 it('refuses a cancelled ticket disguised as accepted admission receipt',async()=>{
  const request=body();vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({status:'accepted',selection_key:request.selection_key,start_key:request.start_key,preset_id:request.preset_id,ticket:{download_job_id:uuid(5),status:'cancelled'}}),{status:200})));
  await expect(abandonDownloadPreparation('csrf',request)).rejects.toThrow();
 });
});
