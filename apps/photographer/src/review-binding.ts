import type {Review} from './catalog-types';

const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const digest=/^[0-9a-f]{64}$/;
const zero='00000000-0000-0000-0000-000000000000';

// The server proves adopted ancestry and physical bytes. This check preserves
// its exact render before/after pins across the client decision boundary.
export function ownRenderReviewBinding(value:Review):Review{
 if(!hasBoundCandidatePreview(value.kind))return value;
 const decidedCandidate=value.state==='decided'&&value.revision===2&&((value.decision==='accept'&&value.decision_route==='candidate')||(value.decision==='reject'&&value.decision_route==='base')||(value.decision==='skip'&&value.decision_route==='skip'));
 const valid=(ref:Review['candidate'])=>ref!==undefined&&ref!==null&&typeof ref.artifact_id==='string'&&uuid.test(ref.artifact_id)&&ref.artifact_id!==zero&&typeof ref.sha256==='string'&&digest.test(ref.sha256);
 if(!valid(value.candidate)||!valid(value.base)||value.candidate!.sha256!==value.candidate_hash||value.candidate!.artifact_id===value.base!.artifact_id||!Number.isSafeInteger(value.revision)||value.revision<1||(value.state!=='pending'&&!decidedCandidate))throw new Error('Invalid candidate review binding');
 return {...value,candidate:{...value.candidate!},base:{...value.base!},candidate_ids:value.candidate_ids?[...value.candidate_ids]:undefined};
}

export function hasBoundCandidatePreview(kind:string):boolean{
 return kind==='portrait.hair'||kind==='portrait.clothing'||kind==='render.compose'||kind==='optical.negative'||kind==='portrait.open_eyes'||isRestorationReview(kind);
}

// Restoration uses the same bound candidate shape for pending previews and
// decided selections; its mutation route remains separately gated.
export function isRestorationReview(kind:string):boolean{
 return kind==='portrait.glare'||kind==='image.reflection'||kind==='image.relight';
}
