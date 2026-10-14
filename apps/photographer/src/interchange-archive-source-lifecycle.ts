import type {UploadSource} from '@photoeditor/api-client';
const releases=new WeakMap<UploadSource,()=>void>();
/** Register only a cache copy owned by this selection, never the user's original. */
export function ownArchiveSource(source:UploadSource,release:()=>void):UploadSource {
 let disposed=false;
 const owned:UploadSource={...source,read:async(start,end,signal)=>{
  if(disposed)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_RELEASED');
  const bytes=await source.read(start,end,signal);
  if(disposed)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_RELEASED');
  return bytes;
 }};
 releases.set(owned,()=>{disposed=true;release();});return owned;
}
export function releaseArchiveSource(source:UploadSource):void {
 const release=releases.get(source);if(!release)return;
 // Keep the callback after a filesystem error so an explicit retry can clean it.
 release();releases.delete(source);
}
