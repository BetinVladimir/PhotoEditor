import type {Collection,CollectionItem} from './catalog-types';
import type {MultiApplyRecord} from './multi-apply-attempt';
import {freezeCollectionVersionIntent} from './collection-version';
/** Select exactly the frozen reference occurrence; never rebase an in-flight CAS. */
export function multiCollectionSelection(collection:Collection,items:CollectionItem[],reference:string):MultiApplyRecord['selection']{
 const matches=items.filter(item=>item.collection_id===collection.id&&item.asset_version_id===reference);
 if(matches.length!==1)return null;
 const item=matches[0];
 const selection={collection:collection.id,item:item.id,asset:item.asset_id,revision:collection.revision,expectedVersion:reference};
 return freezeCollectionVersionIntent(selection);
}
