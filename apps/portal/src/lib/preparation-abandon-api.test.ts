import {afterEach,expect,it,vi} from 'vitest';
import {abandonDownloadPreparation} from './api';
const preset='11111111-1111-4111-8111-111111111111',collection='22222222-2222-4222-8222-222222222222',job='33333333-3333-4333-8333-333333333333';
const command=()=>({selection_key:'selection-original',start_key:'start-original',selection:{collection_revision:'1',scope:'all'},preset_id:preset,snapshot_id:null});
const abandoned={status:'abandoned',selection_key:'selection-original',start_key:'start-original',preset_id:preset,collection_id:collection};
afterEach(()=>vi.unstubAllGlobals());
it('owns the original two command keys and body across transport',async()=>{
 const body=command();let sent='';
 vi.stubGlobal('fetch',vi.fn(async(_path,init)=>{sent=String(init?.body);body.selection_key='replacement';body.selection.collection_revision='2';return new Response(JSON.stringify(abandoned),{status:200});}));
 await expect(abandonDownloadPreparation('csrf',body)).resolves.toEqual({status:'abandoned'});
 expect(JSON.parse(sent)).toEqual(command());
});
it('preserves a validated accepted ticket instead of treating it as abandoned',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({status:'accepted',selection_key:'selection-original',start_key:'start-original',preset_id:preset,ticket:{download_job_id:job,status:'queued'}}),{status:200})));
 await expect(abandonDownloadPreparation('csrf',command())).resolves.toEqual({status:'accepted',ticket:{download_job_id:job,status:'queued'}});
});
it.each([{...abandoned,start_key:'foreign'}, {...abandoned,preset_id:collection}, {...abandoned,extra:true}, {...abandoned,collection_id:'invalid'}, {...abandoned,status:'accepted',ticket:{download_job_id:job,status:'ready'}}])('rejects malformed or cross-command resolution',async body=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(body),{status:200})));
 await expect(abandonDownloadPreparation('csrf',command())).rejects.toThrow();
});
it('refuses both pre-abort and a late successful body after abort',async()=>{
 const first=new AbortController();first.abort();const transport=vi.fn();vi.stubGlobal('fetch',transport);
 await expect(abandonDownloadPreparation('csrf',command(),first.signal)).rejects.toMatchObject({name:'AbortError'});expect(transport).not.toHaveBeenCalled();
 const late=new AbortController();vi.stubGlobal('fetch',vi.fn(async()=>({ok:true,status:200,json:async()=>{late.abort();return abandoned;}})));
 await expect(abandonDownloadPreparation('csrf',command(),late.signal)).rejects.toMatchObject({name:'AbortError'});
});
