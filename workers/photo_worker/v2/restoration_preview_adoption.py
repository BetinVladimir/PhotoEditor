"""Full V28 encoded preview validation before fenced receipt commit."""
from photo_worker.contracts.base import canonical_bytes

from .adoption import AdoptionInvalid, ArtifactRef
from .render_derivative_adoption import verify_render_derivative_result
from .render_derivative_service import RenderDerivativeAdoptionCoordinator
from .restoration_preview_reader import PostgresRestorationPreviewReader


class RestorationPreviewAdoptionCoordinator(RenderDerivativeAdoptionCoordinator):
    def _query(self, connection, tenant, sql, args, *, one=False):
        self.check_active()
        self._require_idle_connections()
        with connection.transaction():
            if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
                raise AdoptionInvalid('RESTORATION_PREVIEW_ADOPTER_ROLE_INVALID')
            connection.execute("SELECT set_config('app.tenant_id',%s,true)",
                               (str(tenant),))
            connection.execute("SET LOCAL statement_timeout='15s'")
            self.check_active()
            cursor = connection.execute(sql, args)
            result = cursor.fetchone() if one else cursor.fetchall()
            self.check_active()
        self._require_idle_connections()
        return result

    def adopt(self, tenant_id, run_id, side, variant):
        self.check_active()
        self._require_idle_connections()
        if side not in ('before', 'after') or variant not in ('preview1600', 'preview2560'):
            raise AdoptionInvalid('RESTORATION_PREVIEW_SCOPE_INVALID')
        rows = self._query(self.read, tenant_id,
            'SELECT * FROM photo.v28_restoration_preview_context(%s,%s,%s,%s)',
            (tenant_id, run_id, side, variant))
        if len(rows) != 1:
            raise AdoptionInvalid('RESTORATION_PREVIEW_CONTEXT_STALE')
        task, result, attempt, fence, params_id, params_sha, manifest_id, manifest_sha = rows[0]
        reader = PostgresRestorationPreviewReader(self.read, self.objects,
            tenant_id=tenant_id, run_id=run_id, task_id=task, result_id=result,
            side=side, variant=variant, attempt=attempt, fence=fence,
            allowed_bucket=self.bucket)
        options = {}
        if self.additional_working_sha:
            options['additional_working_icc_sha256'] = self.additional_working_sha
        manifest = verify_render_derivative_result(reader,
            ArtifactRef(params_id, params_sha), ArtifactRef(manifest_id, manifest_sha),
            target_icc_sha256=self.target_sha,
            working_icc_sha256=self.working_sha,
            check_active=self.check_active, **options)
        # Full decode and all object IO end before the receipt transaction.
        body = canonical_bytes(manifest).decode('utf-8')
        locations = canonical_bytes(reader.verified_locations()).decode('utf-8')
        row = self._query(self.write, tenant_id,
            'SELECT photo.record_v28_restoration_preview_derivative_receipt'
            '(%s,%s,%s,%s,%s,%s,%s,%s,%s::jsonb)',
            (tenant_id, run_id, side, variant, result, attempt, fence,
             body, locations), one=True)
        if row != (True,):
            raise AdoptionInvalid('RESTORATION_PREVIEW_COMMIT_STALE')
        return True
