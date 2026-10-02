import {expect,it,vi,beforeEach} from 'vitest';
const stages=vi.hoisted(()=>({load:vi.fn(),display:vi.fn()}));
vi.mock('./region-preview-load',()=>({loadRegionPreview:stages.load}));
vi.mock('./region-preview-display',()=>({prepareRegionDisplay:stages.display}));
import {createRegionLoupeLoader} from './region-loupe-loader';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){
 const request={version_id:id(1),recipe:{artifact_id:id(2),sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:1024},source={artifact_id:id(3),sha256:'b'.repeat(64)};
 const ticket={run_id:id(4),workflow_id:id(5),state:'queued' as const};
 const binding={request:structuredClone(request),source:{...source},canvas:{width:2000,height:1000},paramsSha256:'c'.repeat(64),iccSha256:'d'.repeat(64),alpha:false};
 const api={regionPreview:vi.fn().mockResolvedValue(ticket),regionDescriptor:vi.fn().mockResolvedValue(binding),run:vi.fn(),runOutputs:vi.fn()};return {api,request,source,ticket,binding};
}
beforeEach(()=>{vi.resetAllMocks();stages.load.mockResolvedValue({manifest:{},encodedBytes:new Uint8Array([1,2])});stages.display.mockResolvedValue({uri:'blob:verified'});});
it('retries admission with the captured body/key and reuses the admitted ticket after a download failure',async()=>{
 const f=fixture(),loader=createRegionLoupeLoader(f.api,id(6),f.request,f.source,'key');
 f.request.rect.x1=.5;f.source.sha256='f'.repeat(64);f.api.regionPreview.mockRejectedValueOnce(new Error('lost response'));
 await expect(loader(new AbortController().signal)).rejects.toThrow('lost response');
 stages.load.mockRejectedValueOnce(new Error('download'));
 await expect(loader(new AbortController().signal)).rejects.toThrow('download');
 await loader(new AbortController().signal);
 expect(f.api.regionPreview).toHaveBeenCalledTimes(2);expect(f.api.regionPreview.mock.calls[0].slice(0,3)).toEqual(f.api.regionPreview.mock.calls[1].slice(0,3));
 expect(f.api.regionPreview.mock.calls[1][1].rect.x1).toBe(1);expect(f.api.regionDescriptor.mock.calls[0][3].sha256).toBe('b'.repeat(64));
 expect(f.api.regionDescriptor).toHaveBeenCalledTimes(2);expect(stages.display).toHaveBeenCalledTimes(1);
});
it('does not download outputs or decode when descriptor authorization fails',async()=>{
 const f=fixture();f.api.regionDescriptor.mockRejectedValue(new Error('revoked'));
 await expect(createRegionLoupeLoader(f.api,id(6),f.request,f.source,'key')(new AbortController().signal)).rejects.toThrow('revoked');
 expect(stages.load).not.toHaveBeenCalled();expect(stages.display).not.toHaveBeenCalled();
});
it('does not continue after an aborted admission whose transport ignores cancellation',async()=>{
 const f=fixture(),controller=new AbortController();f.api.regionPreview.mockImplementation(async()=>{controller.abort();return f.ticket;});
 await expect(createRegionLoupeLoader(f.api,id(6),f.request,f.source,'key')(controller.signal)).rejects.toThrow();
 expect(f.api.regionDescriptor).not.toHaveBeenCalled();expect(stages.load).not.toHaveBeenCalled();
});
