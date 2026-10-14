import {expect,it,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {prepareReviewPreviewDisplay,prepareReviewPreviewPair} from './review-preview-display';
import type {ReviewPreviewBytes} from './review-preview';
function value():ReviewPreviewBytes{const bytes=new Uint8Array([1,2,3]);return {profile:'b'.repeat(64),source:{artifact_id:'00000000-0000-4000-8000-000000000001',sha256:'a'.repeat(64)},width:2,height:1,bytes,image:{artifact_id:'00000000-0000-4000-8000-000000000002',sha256:bytesToHex(sha256(bytes)),bytes:3,role:'derivative',media_type:'image/jpeg',url:'https://media.test/image',expires_at:'2030-01-01T00:00:00Z'}}}
it('displays the exact verified bytes and decoder geometry',async()=>{const decode=vi.fn(async()=>({width:2,height:1})),result=await prepareReviewPreviewDisplay(value(),new AbortController().signal,decode);expect(result.uri).toBe('data:image/jpeg;base64,AQID');expect(decode).toHaveBeenCalledWith(result.uri)});
it('rejects changed encoded bytes before decoding',async()=>{const v=value();v.bytes[0]=4;const decode=vi.fn();await expect(prepareReviewPreviewDisplay(v,new AbortController().signal,decode)).rejects.toThrow();expect(decode).not.toHaveBeenCalled()});
it('refuses decoder dimensions and cancellation during decode',async()=>{await expect(prepareReviewPreviewDisplay(value(),new AbortController().signal,async()=>({width:3,height:1}))).rejects.toThrow();const c=new AbortController();await expect(prepareReviewPreviewDisplay(value(),c.signal,async()=>{c.abort();return {width:2,height:1}})).rejects.toThrow('REVIEW_PREVIEW_ABORTED')});

it('binds both review displays to the same profile and geometry before decode',async()=>{
 const before=value(),after=value();after.source={...after.source,artifact_id:'00000000-0000-4000-8000-000000000003'};
 const decode=vi.fn(async()=>({width:2,height:1}));
 expect(await prepareReviewPreviewPair(before,after,new AbortController().signal,decode)).toHaveLength(2);
 for(const changed of [{...after,profile:'c'.repeat(64)},{...after,width:3},{...after,height:2},{...after,source:{...before.source}}]){
  decode.mockClear();await expect(prepareReviewPreviewPair(before,changed,new AbortController().signal,decode)).rejects.toThrow('REVIEW_PREVIEW_PAIR_INVALID');expect(decode).not.toHaveBeenCalled();
 }
});
