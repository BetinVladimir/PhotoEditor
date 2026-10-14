import {expect,it,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {interchangeArchiveSource,maxInterchangeArchiveBytes} from './interchange-archive-source';
function file(size=1024*1024+22){const data=new Uint8Array(size).fill(7);return {data,name:'bundle.zip',size,lastModified:10,slice:vi.fn((start:number,end:number)=>({arrayBuffer:async()=>data.slice(start,end).buffer}))};}
it('hashes bounded chunks and reads multipart ranges without whole-file buffering',async()=>{
 const input=file(),progress=vi.fn(),source=await interchangeArchiveSource(input,new AbortController().signal,progress);
 expect(source.sha256).toBe(bytesToHex(sha256(input.data)));expect(input.slice.mock.calls).toEqual([[0,1048576],[1048576,input.size]]);
 expect(progress.mock.calls.at(-1)).toEqual([input.size,input.size]);expect(await source.read(1,8,new AbortController().signal)).toEqual(input.data.slice(1,8));
});
it.each(['name','limit','fraction','truncated'])('rejects invalid or changing source: %s',async mode=>{
 const input=file(22);if(mode==='name')input.name='../bundle.zip';if(mode==='limit')input.size=maxInterchangeArchiveBytes+1;if(mode==='fraction')input.size=22.5;
 if(mode==='truncated')input.slice.mockImplementation(()=>({arrayBuffer:async()=>new Uint8Array(1).buffer}));
 await expect(interchangeArchiveSource(input,new AbortController().signal)).rejects.toThrow();
 if(mode!=='truncated')expect(input.slice).not.toHaveBeenCalled();
});
it('cancels hashing after an in-flight chunk without delivering progress or source',async()=>{
 const input=file(22),controller=new AbortController(),progress=vi.fn();input.slice.mockImplementation(()=>({arrayBuffer:async()=>{controller.abort();return input.data.buffer}}));
 await expect(interchangeArchiveSource(input,controller.signal,progress)).rejects.toThrow();expect(progress).not.toHaveBeenCalled();
});
it('refuses replaced file metadata and invalid read ranges',async()=>{
 const input=file(22),source=await interchangeArchiveSource(input,new AbortController().signal);input.lastModified++;
 await expect(source.read(0,22,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');input.lastModified--;
 await expect(source.read(-1,22,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_RANGE_INVALID');
});

it('uses Unicode rune bounds and rejects C1 control characters before IO',async()=>{
 const input=file(22);input.name='😀'.repeat(200)+'.zip';
 await expect(interchangeArchiveSource(input,new AbortController().signal)).resolves.toBeDefined();
 const invalid=file(22);invalid.name='a\u0085.zip';await expect(interchangeArchiveSource(invalid,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_INVALID');expect(invalid.slice).not.toHaveBeenCalled();
});
