import {expect,it} from 'vitest';
import {pendingReviewImageToken,readRelightViewedReceipt} from './relight-viewed-receipt';
it('accepts only exact same-origin pending-image token URLs',()=>{
 const origin='https://photo.invalid',url=origin+'/api/v1/media/review-pending?token=before.signed&versionId=v1';
 expect(pendingReviewImageToken(url,origin)).toBe('before.signed');
 for(const bad of [url.replace('photo.invalid','other.invalid'),url+'&token=duplicate',url+'&extra=1',url+'#fragment',url.replace('review-pending','render-selected'),url.replace('before.signed','')])expect(()=>pendingReviewImageToken(bad,origin)).toThrow('PAIR_INVALID');
});
it('reads a bounded live receipt and rejects expiry or extra fields',()=>{
 const now=Date.parse('2026-10-16T12:00:00Z'),receipt={version:1,token:'viewed.signed',expires_at:'2026-10-16T12:01:00Z'};
 expect(readRelightViewedReceipt(receipt,now)).toEqual(receipt);
 for(const bad of [{...receipt,version:2},{...receipt,extra:true},{...receipt,token:'a'.repeat(4201)},{...receipt,expires_at:'2026-10-16T12:00:00Z'},{...receipt,expires_at:'2026-10-16T12:03:00Z'},{...receipt,expires_at:'invalid'}])expect(()=>readRelightViewedReceipt(bad,now)).toThrow('RECEIPT_INVALID');
});

it('requires the explicit Go time wire shape while retaining nanoseconds and offsets',()=>{
 const now=Date.parse('2026-10-16T12:00:00Z'),receipt={version:1,token:'viewed.signed',expires_at:'2026-10-16T12:01:00.123456789Z'};
 expect(readRelightViewedReceipt(receipt,now)).toEqual(receipt);
 expect(readRelightViewedReceipt({...receipt,expires_at:'2026-10-16T14:01:00+02:00'},now).expires_at).toBe('2026-10-16T14:01:00+02:00');
 for(const expires_at of ['2026-10-16T12:01:00','2026-10-16T12:01Z','2026-10-16 12:01:00Z'])expect(()=>readRelightViewedReceipt({...receipt,expires_at},now)).toThrow('RECEIPT_INVALID');
});
