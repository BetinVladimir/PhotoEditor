import {expect,it} from 'vitest';
import type {Review} from './catalog-types';
import {hasBoundCandidatePreview,isRestorationReview,ownRenderReviewBinding} from './review-binding';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
function review():Review{return {id:id(1),run_id:id(2),kind:'render.compose',state:'pending',revision:1,candidate_hash:'a'.repeat(64),candidate:{artifact_id:id(3),sha256:'a'.repeat(64)},base:{artifact_id:id(4),sha256:'b'.repeat(64)},reasons:[],created_at:'2026-10-10T00:00:00Z'}};
it('owns exact before/after pins',()=>{const source=review(),owned=ownRenderReviewBinding(source);source.candidate!.sha256='c'.repeat(64);expect(owned.candidate!.sha256).toBe('a'.repeat(64));expect(owned.base).not.toBe(source.base)});
it.each(['missing','hash','alias','zero','revision','state'])('refuses invalid render binding: %s',mode=>{const value=review();if(mode==='missing')delete value.base;if(mode==='hash')value.candidate_hash='c'.repeat(64);if(mode==='alias')value.base!.artifact_id=value.candidate!.artifact_id;if(mode==='zero')value.candidate!.artifact_id='00000000-0000-0000-0000-000000000000';if(mode==='revision')value.revision=Number.MAX_SAFE_INTEGER+1;if(mode==='state')value.state='decided';expect(()=>ownRenderReviewBinding(value)).toThrow()});
it('preserves other review contracts',()=>{const value=review();value.kind='defects';delete value.base;expect(ownRenderReviewBinding(value)).toBe(value)});

it('binds V28 preview refs for pending and decided restoration reviews',()=>{
 expect(isRestorationReview('portrait.glare')).toBe(true);
 expect(isRestorationReview('image.reflection')).toBe(true);
 expect(isRestorationReview('render.compose')).toBe(false);
 for(const kind of ['portrait.glare','image.reflection']){
  expect(hasBoundCandidatePreview(kind)).toBe(true);
  const value=review();value.kind=kind;
  const owned=ownRenderReviewBinding(value);
  expect(owned.candidate).toEqual(value.candidate);
  expect(owned.candidate).not.toBe(value.candidate);
  value.candidate_hash='c'.repeat(64);
  expect(()=>ownRenderReviewBinding(value)).toThrow();
 }
});
it('retains a decided render review only with an exact route and revision',()=>{
 const value=review();value.state='decided';value.revision=2;value.decision='reject';value.decision_route='base';
 const owned=ownRenderReviewBinding(value);expect(owned.base).toEqual(value.base);expect(owned.base).not.toBe(value.base);
 value.decision_route='candidate';expect(()=>ownRenderReviewBinding(value)).toThrow();
});
