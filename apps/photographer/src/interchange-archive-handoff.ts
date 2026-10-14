import {canonicalJSON} from '@photoeditor/api-client';
import type {InterchangeArchiveAttempt} from './interchange-archive-attempt-store';
/** A ready response is usable only while its exact durable attempt remains current. */
export function assertArchiveHandoffAttempt(before:InterchangeArchiveAttempt,after:InterchangeArchiveAttempt|null):void {
 if(!before.checkpoint||!after?.checkpoint||before.checkpoint.stage==='aborting'||after.checkpoint.stage==='aborting'||canonicalJSON(before)!==canonicalJSON(after))throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
}
