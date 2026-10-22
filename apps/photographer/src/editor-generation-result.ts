export type GenerationTerminalStatus='failed'|'cancelled'|'completed_with_errors';
export class GenerationTerminalError extends Error{
 constructor(readonly status:GenerationTerminalStatus){super('GENERATION_RUN_TERMINAL');this.name='GenerationTerminalError';}
}
export function generationResultState(status:string):'outputs'|'pending'|'terminal'{
 if(['completed','waiting_review','completed_with_errors'].includes(status))return 'outputs';
 if(['failed','cancelled'].includes(status))return 'terminal';
 if(['draft','queued','running','cancelling'].includes(status))return 'pending';
 throw new Error('GENERATION_RUN_STATE_INVALID');
}
