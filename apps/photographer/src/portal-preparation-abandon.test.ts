// @vitest-environment jsdom
import {describe,it,expect,vi} from 'vitest';
import {prepareDownload,abandonPreparation,hasPreparation} from '../../portal/src/lib/download-preparation';
const id=(n:number)=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const storage=()=>{const map=new Map<string,string>();return {map,getItem:(k:string)=>map.get(k)??null,setItem:(k:string,v:string)=>{map.set(k,v);},removeItem:(k:string)=>{map.delete(k);}};};
describe('Portal immutable uncertain preparation',()=>{
 it.each(['IDEMPOTENCY_CONFLICT','VALIDATION_FAILED'])('keeps exact keys after unknown then %s',async code=>{
  const store=storage(),share=crypto.randomUUID();const api={createSelection:vi.fn().mockRejectedValueOnce(new Error('lost response')).mockRejectedValueOnce(new Error(code)),startDownload:vi.fn()};
  const request={revision:'1',preset:id(2)};
  await expect(prepareDownload(api,store,share,'csrf',request,new AbortController().signal)).rejects.toThrow();
  const raw=[...store.map.values()][0];
  await expect(prepareDownload(api,store,share,'csrf',{...request,revision:'2'},new AbortController().signal)).rejects.toThrow();
  expect([...store.map.values()][0]).toBe(raw);expect(api.createSelection.mock.calls[1][4]).toBe(api.createSelection.mock.calls[0][4]);
 });
 it.each(['{broken',JSON.stringify({revision:'1'})])('preserves corrupt raw without producing %s',async raw=>{
  const store=storage(),share=crypto.randomUUID();store.setItem(`photo:portal:${share}:download-preparation`,raw);
  const api={createSelection:vi.fn(),startDownload:vi.fn()};expect(hasPreparation(store,share)).toBe(true);
  await expect(prepareDownload(api,store,share,'csrf',{revision:'1',preset:id(2)},new AbortController().signal)).rejects.toThrow('RECOVERY_UNAVAILABLE');
  expect(store.getItem(`photo:portal:${share}:download-preparation`)).toBe(raw);expect(api.createSelection).not.toHaveBeenCalled();
 });
 it('does not rekey when storage cannot be read',async()=>{
  const store={getItem:()=>{throw Error('storage');},setItem:vi.fn(),removeItem:vi.fn()},api={createSelection:vi.fn(),startDownload:vi.fn()};
  await expect(prepareDownload(api,store,crypto.randomUUID(),'csrf',{revision:'1',preset:id(2)},new AbortController().signal)).rejects.toThrow();expect(api.createSelection).not.toHaveBeenCalled();expect(store.removeItem).not.toHaveBeenCalled();
 });
 it('persists the same memory keys before any new POST after write failure',async()=>{
  const base=storage(),share=crypto.randomUUID();let failing=true;
  const store={...base,setItem:(k:string,v:string)=>{if(failing)throw Error('storage');base.setItem(k,v);}};
  const api={createSelection:vi.fn().mockRejectedValue(Error('network')),startDownload:vi.fn()};
  await expect(prepareDownload(api,store,share,'csrf',{revision:'1',preset:id(2)},new AbortController().signal)).rejects.toThrow('storage');expect(api.createSelection).not.toHaveBeenCalled();
  failing=false;await expect(prepareDownload(api,store,share,'csrf',{revision:'2',preset:id(2)},new AbortController().signal)).rejects.toThrow('network');expect(api.createSelection.mock.calls[0][1]).toBe('1');
 });
 it('accepted receipt wins resolution; only explicit ack clears matching intent',async()=>{
  const store=storage(),share=crypto.randomUUID(),controller=new AbortController();
  const api={createSelection:vi.fn().mockResolvedValue({id:id(3),item_count:1}),startDownload:vi.fn().mockRejectedValue(Error('lost'))};
  await expect(prepareDownload(api,store,share,'csrf',{revision:'1',preset:id(2)},controller.signal)).rejects.toThrow();
  const abandonDownloadPreparation=vi.fn().mockResolvedValue({status:'accepted',ticket:{download_job_id:id(4),status:'queued'}});
  const out=await abandonPreparation({abandonDownloadPreparation},store,share,'csrf',controller.signal);
  expect(out.result.status).toBe('accepted');expect(hasPreparation(store,share)).toBe(true);out.clear();expect(hasPreparation(store,share)).toBe(false);
  expect(abandonDownloadPreparation.mock.calls[0][1].snapshot_id).toBe(id(3));
 });
});

it('renewed guest scope preserves original unknown journal and cannot clear it',async()=>{
 const {preparationScope}=await import('../../portal/src/lib/download-preparation');
 const store=storage(),share=crypto.randomUUID(),original=preparationScope(share,'old'),renewed=preparationScope(share,'new');
 const api={createSelection:vi.fn().mockRejectedValue(Error('lost')),startDownload:vi.fn()};
 await expect(prepareDownload(api,store,original,'old',{revision:'1',preset:id(2)},new AbortController().signal)).rejects.toThrow();
 const raw=[...store.map.values()][0];
 const resolve={abandonDownloadPreparation:vi.fn()};
 await expect(abandonPreparation(resolve,store,renewed,'new',new AbortController().signal)).rejects.toThrow('PREPARATION_REQUIRED');
 expect(resolve.abandonDownloadPreparation).not.toHaveBeenCalled();expect([...store.map.values()][0]).toBe(raw);
});

it('keeps known accepted ticket when receipt persistence fails; retry makes no POST',async()=>{
 const base=storage(),share=crypto.randomUUID();let fail=true;
 const store={...base,setItem:(k:string,v:string)=>{if(fail&&JSON.parse(v).ticket)throw Error('receipt storage');base.setItem(k,v);}};
 const api={createSelection:vi.fn().mockResolvedValue({id:id(3),item_count:1}),startDownload:vi.fn().mockResolvedValue({download_job_id:id(4),status:'queued'})};
 await expect(prepareDownload(api,store,share,'csrf',{revision:'1',preset:id(2)},new AbortController().signal)).rejects.toThrow('receipt storage');
 fail=false;expect((await prepareDownload(api,store,share,'csrf',{revision:'99',preset:id(2)},new AbortController().signal)).download_job_id).toBe(id(4));
 expect(api.createSelection).toHaveBeenCalledTimes(1);expect(api.startDownload).toHaveBeenCalledTimes(1);
});
