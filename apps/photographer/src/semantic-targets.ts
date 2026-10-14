import type en from '../../../locales/en/common.json';
import type {SemanticTargetResource,SemanticTargetState} from '@photoeditor/api-client';
export const semanticTargetLabels:Record<SemanticTargetState,string>={proposed:'Ready for review',queued:'Processing',rejected:'Rejected',unsupported:'Unsupported edit',applied:'Applied',failed:'Processing failed',cancelled:'Cancelled'};
// Decimal revisions are compared without conversion to JavaScript numbers.
export function mergeSemanticTargets(current:SemanticTargetResource[],incoming:SemanticTargetResource[]):SemanticTargetResource[]{
 if(current.length>5000||incoming.length>5000)throw new Error('SEMANTIC_TARGET_LIMIT');
 const values=new Map(current.map(item=>[item.id,item]));
 for(const item of incoming){const old=values.get(item.id);if(!old||item.revision.length>old.revision.length||item.revision.length===old.revision.length&&item.revision>=old.revision)values.set(item.id,item);if(values.size>5000)throw new Error('SEMANTIC_TARGET_LIMIT')}
 return [...values.values()];
}

export const semanticTargetLabelKeys:Record<SemanticTargetState,keyof typeof en>={proposed:'semantic_target_proposed',queued:'semantic_target_queued',rejected:'semantic_target_rejected',unsupported:'semantic_target_unsupported',applied:'semantic_target_applied',failed:'semantic_target_failed',cancelled:'semantic_target_cancelled'};
