import type {CollectionItem} from './catalog-types';
import {readGalleryItem} from './collection-gallery-api';
import {bulkUUID} from './collection-bulk-api';

/** The mutation returns the same full Item wire DTO as the owner gallery. */
export function readCollectionItemResponse(raw:unknown,expected:{collection:string;item?:string;asset?:string;version?:string;moderation?:string}):CollectionItem{
 if(!bulkUUID(expected.collection))throw Error('COLLECTION_ITEM_RECEIPT_INVALID');
 const value=readGalleryItem(raw,expected.collection);
 if(expected.item!==undefined&&value.id!==expected.item||expected.asset!==undefined&&value.asset_id!==expected.asset||expected.version!==undefined&&value.asset_version_id!==expected.version||expected.moderation!==undefined&&value.moderation_state!==expected.moderation)throw Error('COLLECTION_ITEM_RECEIPT_INVALID');
 return value;
}
