import {it,expect,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readRegionArtifact} from './region-preview-download';
import type {RunOutput} from './catalog-types';
const bytes=new TextEncoder().encode('{"ok":true}');
const ref=():RunOutput=>({artifact_id:'11111111-1111-4111-8111-111111111111',role:'document',media_type:'application/json',bytes:bytes.length,sha256:bytesToHex(sha256(bytes)),url:'https://photo.test/signed?versionId=immutable',expires_at:'2099-01-01'});
function transport(response:Response){return vi.fn().mockResolvedValue(response) as unknown as typeof fetch;}
it('streams verified bytes without forwarding API credentials',async()=>{
 const fetcher=transport(new Response(bytes));const signal=new AbortController().signal;
 expect(await readRegionArtifact(ref(),'manifest',signal,fetcher)).toEqual(bytes);expect(fetcher).toHaveBeenCalledWith(ref().url,expect.objectContaining({signal,credentials:'omit',redirect:'error',cache:'no-store'}));
});
it('rejects oversized declarations and expired/invalid links before fetching',async()=>{
 const fetcher=vi.fn() as unknown as typeof fetch;
 for(const change of [{bytes:8388609},{expires_at:'2000-01-01'},{url:'file:///source'},{url:'http://photo.test/signed?versionId=v1'},{url:'https://user:pass@photo.test/x'},{url:'https://photo.test/signed'},{url:'https://photo.test/signed?versionId=null'},{url:'https://photo.test/signed?versionId=a&versionId=b'},{role:'source'}])await expect(readRegionArtifact({...ref(),...change},'manifest',new AbortController().signal,fetcher)).rejects.toThrow();expect(fetcher).not.toHaveBeenCalled();
});
it('allows a versioned loopback object link',async()=>{
 const local={...ref(),url:'http://127.0.0.1:9000/signed?versionId=v1'};
 expect(await readRegionArtifact(local,'manifest',new AbortController().signal,transport(new Response(bytes)))).toEqual(bytes);
});
it('downloads review media through an API capability without bearer credentials or referrer',async()=>{
 const capability={...ref(),role:'derivative_manifest',url:'https://api.photo.test/api/v1/media/review-pending?token=sealed&versionId=v1'};
 const fetcher=transport(new Response(bytes));
 expect(await readRegionArtifact(capability,'review_manifest',new AbortController().signal,fetcher)).toEqual(bytes);
 expect(fetcher).toHaveBeenCalledWith(capability.url,expect.objectContaining({credentials:'omit',redirect:'error',referrerPolicy:'no-referrer',cache:'no-store'}));
});
it('rejects length mismatch, truncation, extra bytes and wrong SHA',async()=>{
 for(const response of [new Response(bytes,{headers:{'content-length':'1'}}),new Response(bytes.slice(0,-1)),new Response(new Uint8Array(bytes.length+1)),new Response(new Uint8Array(bytes.length))])await expect(readRegionArtifact(ref(),'manifest',new AbortController().signal,transport(response))).rejects.toThrow('REGION_ARTIFACT_');
});
it('cancels the reader on overflow and abort during stalled input',async()=>{
 const cancelled=vi.fn();const overflow=new ReadableStream<Uint8Array>({start(controller){controller.enqueue(new Uint8Array(bytes.length+1));},cancel:cancelled});
 await expect(readRegionArtifact(ref(),'manifest',new AbortController().signal,transport(new Response(overflow)))).rejects.toThrow('SIZE');expect(cancelled).toHaveBeenCalledOnce();
 const controller=new AbortController();let entered!:()=>void;const reading=new Promise<void>(resolve=>{entered=resolve;});const cancel=vi.fn();const stalled=new ReadableStream<Uint8Array>({pull(){entered();},cancel});
 const result=readRegionArtifact(ref(),'manifest',controller.signal,transport(new Response(stalled)));await reading;controller.abort();await expect(result).rejects.toThrow('ABORTED');expect(cancel).toHaveBeenCalledOnce();
});

it('rejects empty stream blocks and releases the reader after cancellation',async()=>{
 const cancel=vi.fn();const stream=new ReadableStream<Uint8Array>({start(controller){controller.enqueue(new Uint8Array(0));},cancel});
 await expect(readRegionArtifact(ref(),'manifest',new AbortController().signal,transport(new Response(stream)))).rejects.toThrow('STREAM_INVALID');
 expect(cancel).toHaveBeenCalledOnce();expect(stream.locked).toBe(false);
});
