"""Full decode outside transactions, followed by locked locator-fenced receipt."""
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid, ArtifactRef
from .render_derivative_service import RenderDerivativeAdoptionCoordinator
from .render_derivative_adoption import verify_render_derivative_result
from .perspective_preview_reader import PostgresPerspectivePreviewReader


class PerspectivePreviewAdoptionCoordinator(RenderDerivativeAdoptionCoordinator):
    def _query(self, connection, tenant, sql, args, *, one=False):
        self.check_active()
        self._require_idle_connections()
        with connection.transaction():
            if connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
                raise AdoptionInvalid('PERSPECTIVE_PREVIEW_ADOPTER_ROLE_INVALID')
            connection.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
            connection.execute("SET LOCAL statement_timeout='15s'")
            self.check_active()
            cursor = connection.execute(sql, args)
            result = cursor.fetchone() if one else cursor.fetchall()
            self.check_active()
        self._require_idle_connections()
        return result

    def adopt(self, tenant_id, run_id, variant):
        self.check_active()
        self._require_idle_connections()
        if variant not in ('preview1600', 'preview2560'):
            raise AdoptionInvalid('PERSPECTIVE_PREVIEW_VARIANT_INVALID')
        rows = self._query(self.read, tenant_id,
            'SELECT * FROM photo.v29_perspective_preview_context(%s,%s,%s)',
            (tenant_id, run_id, variant))
        if len(rows) != 1:
            raise AdoptionInvalid('PERSPECTIVE_PREVIEW_CONTEXT_STALE')
        task, result, attempt, fence, params_id, params_sha, manifest_id, manifest_sha = rows[0]
        reader = PostgresPerspectivePreviewReader(self.read, self.objects,
            tenant_id=tenant_id, run_id=run_id, task_id=task, result_id=result,
            variant=variant, attempt=attempt, fence=fence, allowed_bucket=self.bucket)
        try:
            manifest = verify_render_derivative_result(reader,
                ArtifactRef(params_id, params_sha), ArtifactRef(manifest_id, manifest_sha),
                target_icc_sha256=self.target_sha, working_icc_sha256=self.working_sha,
                check_active=self.check_active)
        except AdoptionInvalid as exc:
            code = str(exc)
            if code not in {'RENDER_DERIVATIVE_ENCODING_INVALID',
                            'RENDER_DERIVATIVE_CONTRACT_INVALID'}:
                raise
            row = self._query(self.write, tenant_id,
                'SELECT photo.reject_v29_perspective_preview(%s,%s,%s,%s,%s,%s,%s)',
                (tenant_id, run_id, variant, result, attempt, fence, code), one=True)
            if row != (True,):
                raise AdoptionInvalid('PERSPECTIVE_PREVIEW_COMMIT_STALE') from exc
            return False
        # Encoding/ICC/schema/decoded bounds have all been checked before TX.
        body = canonical_bytes(manifest).decode('utf-8')
        locations = canonical_bytes(reader.verified_locations()).decode('utf-8')
        row = self._query(self.write, tenant_id,
            'SELECT photo.record_v29_perspective_preview_receipt'
            '(%s,%s,%s,%s,%s,%s,%s,%s::jsonb)',
            (tenant_id, run_id, variant, result, attempt, fence, body, locations), one=True)
        if row != (True,):
            raise AdoptionInvalid('PERSPECTIVE_PREVIEW_COMMIT_STALE')
        return True
