export type SemanticChoices=Record<string,'approve'|'reject'>;
/** Apply the bound to the latest state, including batched React updates. */
export function toggleSemanticDecision(current:SemanticChoices,id:string,decision:'approve'|'reject'):SemanticChoices{
 if(!Object.hasOwn(current,id)&&Object.keys(current).length>=100)return current;
 const next={...current};if(next[id]===decision)delete next[id];else next[id]=decision;return next;
}
