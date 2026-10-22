export type ContactSheetAccepted={run_id:string;status:'queued'};

// Match the server's immutable ContactSheet Accepted receipt. A current run
// projection is a separate GET and cannot substitute for admission evidence.
export function readContactSheetAccepted(raw:unknown):ContactSheetAccepted{
 if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).sort().join(',')!=='run_id,status')throw new Error('CONTACT_SHEET_RECEIPT_INVALID');
 const value=raw as Record<string,unknown>;
 if(typeof value.run_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value.run_id)||value.run_id==='00000000-0000-0000-0000-000000000000'||value.status!=='queued')throw new Error('CONTACT_SHEET_RECEIPT_INVALID');
 return {run_id:value.run_id,status:'queued'};
}

export type ContactSheetAdmission={selection_snapshot_id:string;locale:'en'|'ru'|'bg';columns:number;rows:number};
export function freezeContactSheetAdmission(selection:string,locale:string,columns:number,rows:number):ContactSheetAdmission{
 if(typeof selection!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(selection)||selection==='00000000-0000-0000-0000-000000000000'||!['en','ru','bg'].includes(locale)||!Number.isInteger(columns)||columns<1||columns>8||!Number.isInteger(rows)||rows<1||rows>10)throw Error('CONTACT_SHEET_REQUEST_INVALID');return {selection_snapshot_id:selection,locale:locale as ContactSheetAdmission['locale'],columns,rows};
}
