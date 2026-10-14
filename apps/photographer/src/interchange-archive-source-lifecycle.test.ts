import {expect,it,vi} from 'vitest';
import type {UploadSource} from '@photoeditor/api-client';
import {ownArchiveSource,releaseArchiveSource} from './interchange-archive-source-lifecycle';
const source=(read:UploadSource['read']):UploadSource=>({name:'archive.zip',size:22,lastModified:1,sha256:'a'.repeat(64),read});
it('releases an owned source once and refuses further reads',async()=>{
 const read=vi.fn(async()=>new Uint8Array(22)),release=vi.fn();const owned=ownArchiveSource(source(read),release);
 await owned.read(0,22,new AbortController().signal);releaseArchiveSource(owned);releaseArchiveSource(owned);
 expect(release).toHaveBeenCalledTimes(1);await expect(owned.read(0,22,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_RELEASED');expect(read).toHaveBeenCalledTimes(1);
});
it('never delivers an in-flight read after release',async()=>{
 let resolve!:(bytes:Uint8Array)=>void;const owned=ownArchiveSource(source(()=>new Promise(done=>{resolve=done;})),vi.fn());
 const pending=owned.read(0,22,new AbortController().signal);releaseArchiveSource(owned);resolve(new Uint8Array(22));
 await expect(pending).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_RELEASED');
});
it('retries failed deletion while keeping reads fenced',async()=>{
 const release=vi.fn().mockImplementationOnce(()=>{throw new Error('disk');});const owned=ownArchiveSource(source(async()=>new Uint8Array(22)),release);
 expect(()=>releaseArchiveSource(owned)).toThrow('disk');await expect(owned.read(0,22,new AbortController().signal)).rejects.toThrow();releaseArchiveSource(owned);expect(release).toHaveBeenCalledTimes(2);
});
it('does not delete unowned web or external sources',()=>{
 expect(()=>releaseArchiveSource(source(async()=>new Uint8Array(22)))).not.toThrow();
});
