import {expect,it} from 'vitest';
import type {Review} from './catalog-types';
import {readNegativeResultSelection} from './negative-result-selection';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const make=(decision:string):Review=>({id:id(1),run_id:id(2),kind:'optical.negative',state:'decided',revision:2,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},decision,decision_route:decision==='accept'?'candidate':decision==='reject'?'base':'skip',reasons:[],created_at:'2026-10-11T00:00:00Z'});
it.each(['accept','reject','skip'])('binds the selected result of %s',decision=>{
 const review=make(decision),image=decision==='accept'?review.candidate:decision==='reject'?review.base:null;
 const raw={schema_version:1,review_id:review.id,decision_hash:'c'.repeat(64),route:review.decision_route,image};
 const result=readNegativeResultSelection(raw,review);expect(result.image).toEqual(image);if(image)expect(result.image).not.toBe(image);
});
it.each(['review','route','image','hash','extra','pending','revision'])('rejects invalid selection: %s',mode=>{
 const review=make('reject');const raw:Record<string,unknown>={schema_version:1,review_id:review.id,decision_hash:'c'.repeat(64),route:'base',image:review.base};
 if(mode==='review')raw.review_id=id(9);if(mode==='route')raw.route='candidate';if(mode==='image')raw.image=review.candidate;if(mode==='hash')raw.decision_hash='bad';if(mode==='extra')raw.extra=true;if(mode==='pending')review.state='pending';if(mode==='revision')review.revision=3;
 expect(()=>readNegativeResultSelection(raw,review)).toThrow();
});
it('never promotes a skipped image',()=>{const review=make('skip');expect(()=>readNegativeResultSelection({schema_version:1,review_id:review.id,decision_hash:'c'.repeat(64),route:'skip',image:review.candidate},review)).toThrow()});
it('refuses a skipped receipt when its frozen base reference is missing',()=>{const review=make('skip');delete review.base;expect(()=>readNegativeResultSelection({schema_version:1,review_id:review.id,decision_hash:'c'.repeat(64),route:'skip',image:null},review)).toThrow('NEGATIVE_RESULT_SELECTION_INVALID')});
it('refuses a different decided hash even when the image and route agree',()=>{const review=make('reject');review.decision_hash='d'.repeat(64);expect(()=>readNegativeResultSelection({schema_version:1,review_id:review.id,decision_hash:'c'.repeat(64),route:'base',image:review.base},review)).toThrow('NEGATIVE_RESULT_SELECTION_INVALID')});
