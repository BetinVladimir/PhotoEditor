"""Scoped quality validation outside SQL, immutable locator-fenced receipt."""
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid, ArtifactRef
from .render_derivative_service import RenderDerivativeAdoptionCoordinator
from .perspective_quality_reader import PostgresPerspectiveQualityReader
from .perspective_quality_validation import verify_perspective_quality_result


class PerspectiveQualityAdoptionCoordinator(RenderDerivativeAdoptionCoordinator):
    def _query(self, connection, tenant, sql, args, *, one=False):
        self.check_active()
        self._require_idle_connections()
        with connection.transaction():
            if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_ADOPTER_ROLE_INVALID')
            connection.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
            connection.execute("SET LOCAL statement_timeout='15s'")
            self.check_active()
            cursor = connection.execute(sql, args)
            result = cursor.fetchone() if one else cursor.fetchall()
            self.check_active()
        self._require_idle_connections()
        return result

    def adopt(self, tenant_id, run_id):
        self.check_active()
        self._require_idle_connections()
        rows = self._query(self.read, tenant_id,
            'SELECT * FROM photo.v29_perspective_quality_context(%s,%s)',
            (tenant_id, run_id))
        if len(rows) != 1:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_CONTEXT_STALE')
        task, result, attempt, fence, params_id, params_sha, report_id, report_sha, target_id, target_sha = rows[0]
        reader = PostgresPerspectiveQualityReader(self.read, self.objects,
            tenant_id=tenant_id, run_id=run_id, task_id=task, result_id=result,
            attempt=attempt, fence=fence, allowed_bucket=self.bucket)
        try:
            report = verify_perspective_quality_result(reader,
                ArtifactRef(params_id, params_sha), ArtifactRef(report_id, report_sha),
                expected_target=ArtifactRef(target_id, target_sha), check_active=self.check_active)
        except AdoptionInvalid as exc:
            code = str(exc)
            if code not in {'PERSPECTIVE_QUALITY_CONTRACT_INVALID',
                            'PERSPECTIVE_QUALITY_DECISION_INVALID',
                            'PERSPECTIVE_QUALITY_BINDING_INVALID'}:
                raise
            row = self._query(self.write, tenant_id,
                'SELECT photo.reject_v29_perspective_quality(%s,%s,%s,%s,%s,%s)',
                (tenant_id, run_id, result, attempt, fence, code), one=True)
            if row != (True,):
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_COMMIT_STALE') from exc
            return False
        # A valid fail/review report is retained as evidence, never promoted to PASS.
        body = canonical_bytes(report).decode('utf-8')
        locations = canonical_bytes(reader.verified_locations()).decode('utf-8')
        row = self._query(self.write, tenant_id,
            'SELECT photo.record_v29_perspective_quality_receipt'
            '(%s,%s,%s,%s,%s,%s,%s::jsonb)',
            (tenant_id, run_id, result, attempt, fence, body, locations), one=True)
        if row != (True,):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_COMMIT_STALE')
        return True
