import type {Review} from './catalog-types';
import {canonicalJSON} from '@photoeditor/api-client';
import {ownRenderReviewBinding} from './review-binding';

export type NegativeResultSelection={schema_version:1;review_id:string;decision_hash:string;route:'candidate'|'base'|'skip';image:{artifact_id:string;sha256:string}|null};
export function readNegativeResultSelection(value:unknown,review:Review):NegativeResultSelection{
 return readCandidateResultSelection(value,review,'optical.negative','NEGATIVE_RESULT_SELECTION_INVALID');
}
export type RenderResultSelection=NegativeResultSelection;
export function readRenderResultSelection(value:unknown,review:Review):RenderResultSelection{
 return readCandidateResultSelection(value,review,'render.compose','RENDER_RESULT_SELECTION_INVALID');
}
export type RestorationResultSelection=NegativeResultSelection;
export function readRestorationResultSelection(value:unknown,review:Review):RestorationResultSelection{
 if(review.kind!=='portrait.glare'&&review.kind!=='portrait.open_eyes'&&review.kind!=='image.reflection'&&review.kind!=='image.relight')throw new Error('RESTORATION_RESULT_SELECTION_INVALID');
 return readCandidateResultSelection(value,review,review.kind,'RESTORATION_RESULT_SELECTION_INVALID');
}
export function readLocalPortraitResultSelection(value:unknown,review:Review):NegativeResultSelection{
 if(review.kind!=='portrait.hair'&&review.kind!=='portrait.clothing')throw new Error('LOCAL_PORTRAIT_RESULT_SELECTION_INVALID');
 return readCandidateResultSelection(value,review,review.kind,'LOCAL_PORTRAIT_RESULT_SELECTION_INVALID');
}
function readCandidateResultSelection(value:unknown,review:Review,kind:string,code:string):NegativeResultSelection{
 const fail=():never=>{throw new Error(code)};
 if(!value||typeof value!=='object'||Array.isArray(value)||review.kind!==kind||review.state!=='decided'||review.revision!==2)return fail();
 let owned:Review;
 try{owned=ownRenderReviewBinding(review)}catch{return fail()}
 const doc=value as Record<string,unknown>,keys=['schema_version','review_id','decision_hash','route','image'];
 if(Object.keys(doc).length!==keys.length||!keys.every(key=>Object.hasOwn(doc,key))||doc.schema_version!==1||doc.review_id!==owned.id||typeof doc.decision_hash!=='string'||! /^[0-9a-f]{64}$/.test(doc.decision_hash)||owned.decision_hash!==undefined&&owned.decision_hash!==doc.decision_hash)return fail();
 const route=owned.decision==='accept'?'candidate':owned.decision==='reject'?'base':owned.decision==='skip'?'skip':undefined;
 if(!route||doc.route!==route||owned.decision_route!==route)return fail();
 if(route==='skip'){if(doc.image!==null)return fail();return {schema_version:1,review_id:owned.id,decision_hash:doc.decision_hash,route,image:null}}
 const expected=route==='candidate'?owned.candidate:owned.base;
 if(!expected||!doc.image||typeof doc.image!=='object'||Array.isArray(doc.image)||canonicalJSON(doc.image)!==canonicalJSON(expected)||! /^[0-9a-f]{64}$/.test(expected.sha256)||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(expected.artifact_id)||expected.artifact_id==='00000000-0000-0000-0000-000000000000')return fail();
 return {schema_version:1,review_id:owned.id,decision_hash:doc.decision_hash,route,image:{...expected}};
}
