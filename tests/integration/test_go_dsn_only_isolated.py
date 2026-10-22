"""Each selected Go root owns a freshly migrated private database.
No shared empty DB; JSON requires actual root PASS and refuses any nested SKIP.
"""
import hashlib
import json
import os
import re
import signal
import time
from pathlib import Path
import subprocess
from uuid import uuid4
import pytest
import psycopg
from psycopg.conninfo import make_conninfo
from conftest import database as isolated_database_config

CASES = [{'package': './internal/v2people', 'test': 'TestIntegrationPeopleConsentReceiptCASAndRevocation', 'source': 'backend/internal/v2people/consent_family_integration_test.go', 'source_sha256': '1a802b2fadb65236e497a51036d5218c657ca88f64351df1a0a4f26e9356a2c0', 'head_requirement': ['consent_family_integration_test.go:20: isolated current migrated PostgreSQL required']}, {'package': './internal/v2learning', 'test': 'TestIntegrationProfileCreateGetAndListVersions', 'source': 'backend/internal/v2learning/profile_integration_test.go', 'source_sha256': 'bfea3d1df60eac666a38a58fe1dc0423e02634d90f9de95df893243641ab4a03', 'head_requirement': ['profile_integration_test.go:20: isolated PostgreSQL with V23 migrations required']}, {'package': './internal/workflow', 'test': 'TestIntegrationActivateBoundTask', 'source': 'backend/internal/workflow/advance_integration_test.go', 'source_sha256': 'a4d726614cd427ed686ce3834a7273c6b2333ff5c291a88b664be1068b2c4551', 'head_requirement': ['advance_integration_test.go:18: isolated PostgreSQL with migration47 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationArtifactLeaseCannotAttachDeletingOrExpired', 'source': 'backend/internal/workflow/artifact_guard_integration_test.go', 'source_sha256': 'e5fe5acfea6f2671ec8cfca448be5c0410ca243ad199ce99951fdb39af77ee14', 'head_requirement': ['artifact_guard_integration_test.go:18: isolated PostgreSQL with migration71 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCancelIdleTasks', 'source': 'backend/internal/workflow/cancellation_integration_test.go', 'source_sha256': '2e92cb2c7447357f3f51960cbc7dd220a8257379f3f565ac4a646d2d9fc1ea0c', 'head_requirement': ['cancellation_integration_test.go:17: isolated PostgreSQL with migration73 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacitySchedulerContractCatalogDrift', 'source': 'backend/internal/workflow/capacity_contract_integration_test.go', 'source_sha256': 'ea1eb87ba39a98e648fc5b178b3f11991accfab9fbefc1a34fe8b0e7b1db533c', 'head_requirement': ['capacity_contract_integration_test.go:14: isolated fully migrated operator database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityHeadFunctionOwnershipAndScope', 'source': 'backend/internal/workflow/capacity_head_installation_integration_test.go', 'source_sha256': 'ec1681926f6423886b925e619072671a3f8866afe4e0c357d57dd2550885ad76', 'head_requirement': ['capacity_head_installation_integration_test.go:18: isolated fully migrated operator database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityHeadIndexesReady', 'source': 'backend/internal/workflow/capacity_head_installation_integration_test.go', 'source_sha256': 'ec1681926f6423886b925e619072671a3f8866afe4e0c357d57dd2550885ad76', 'head_requirement': ['capacity_head_installation_integration_test.go:42: isolated database migrated through 01785 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityBoundedHeadEmptyCycle', 'source': 'backend/internal/workflow/capacity_head_installation_integration_test.go', 'source_sha256': 'ec1681926f6423886b925e619072671a3f8866afe4e0c357d57dd2550885ad76', 'head_requirement': ['capacity_head_installation_integration_test.go:64: isolated database migrated through 01787 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityHeadExclusionsRejectInvalidArrays', 'source': 'backend/internal/workflow/capacity_head_installation_integration_test.go', 'source_sha256': 'ec1681926f6423886b925e619072671a3f8866afe4e0c357d57dd2550885ad76', 'head_requirement': ['capacity_head_installation_integration_test.go:79: isolated database migrated through 01787 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityHeadSnapshotAndAffinitySemantics', 'source': 'backend/internal/workflow/capacity_head_semantics_integration_test.go', 'source_sha256': 'dfdba4e362c4a36cfa2623f6f5050a821bac651db000a2f4f29a54c032413928', 'head_requirement': ['capacity_head_semantics_integration_test.go:19: isolated database migrated through 01787 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityLeaderExclusiveSession', 'source': 'backend/internal/workflow/capacity_leader_integration_test.go', 'source_sha256': '8a62fea92e890369beeb1f8929df650ac1b0648326100f29c5662ca12620a841', 'head_requirement': ['capacity_leader_integration_test.go:14: isolated test database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCapacityNodeDrainGeneration', 'source': 'backend/internal/workflow/capacity_node_drain_integration_test.go', 'source_sha256': '2dfed168ef4eb95fc8c13245b8f56a0cb731d0c151e47224353363c292b9d3e9', 'head_requirement': ['capacity_node_drain_integration_test.go:16: isolated migrated database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCaptureStartupRequiresQualifiedOperations', 'source': 'backend/internal/workflow/capture_scheduler_integration_test.go', 'source_sha256': '694d0ecd1d86894ffa0bb2e854d217c6f114fc57165459477cdec4d3b89fcd5d', 'head_requirement': ['capture_scheduler_integration_test.go:14: isolated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestIntegrationCaptureStartupExactFunctions', 'source': 'backend/internal/workflow/capture_scheduler_integration_test.go', 'source_sha256': '694d0ecd1d86894ffa0bb2e854d217c6f114fc57165459477cdec4d3b89fcd5d', 'head_requirement': ['capture_scheduler_integration_test.go:49: isolated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestIntegrationWatermarkDerivativeSQLGoProfileParity', 'source': 'backend/internal/workflow/delivery_derivative_profile_parity_test.go', 'source_sha256': '195c1d1009baf10922b67ed3adcc9d0b9d224199f15576a5ebb16436776dbd7e', 'head_requirement': ['delivery_derivative_profile_parity_test.go:51: isolated privileged database migrated through01779 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationWatermarkDerivativeSQLRejectsMalformedResize', 'source': 'backend/internal/workflow/delivery_derivative_profile_parity_test.go', 'source_sha256': '195c1d1009baf10922b67ed3adcc9d0b9d224199f15576a5ebb16436776dbd7e', 'head_requirement': ['delivery_derivative_profile_parity_test.go:93: isolated privileged database migrated through01779 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationWatermarkDerivativeSQLRejectsInvalidSourceScalars', 'source': 'backend/internal/workflow/delivery_derivative_profile_parity_test.go', 'source_sha256': '195c1d1009baf10922b67ed3adcc9d0b9d224199f15576a5ebb16436776dbd7e', 'head_requirement': ['delivery_derivative_profile_parity_test.go:145: isolated privileged database migrated through01779 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationDeliveryPendingChildOwnership', 'source': 'backend/internal/workflow/delivery_observer_disjoint_migration_test.go', 'source_sha256': 'c852aff37bb1dcb03960424ae4e8d132112fd66843259042d9c15fadaca4d386', 'head_requirement': ['delivery_observer_disjoint_migration_test.go:18: isolated database migrated through 01959 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationEditorFinalAuthorityAndUnknownOrigin', 'source': 'backend/internal/workflow/editor_effect_final_sql_integration_test.go', 'source_sha256': '6492b790f98f3dbb1234df4139ac4da22b5a87021cc777fa3b8f4413ddd9f213', 'head_requirement': ['editor_effect_final_sql_integration_test.go:7: isolated database migrated through02474 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationIngestArchiveAfterPreparedOriginal', 'source': 'backend/internal/workflow/ingest_archive_integration_test.go', 'source_sha256': '411c4b7cf1b9377062ac3faf637aa06938891d038c95c51f8f0c14733b65ff09', 'head_requirement': ['ingest_archive_integration_test.go:19: isolated PostgreSQL with migration2443 required']}, {'package': './internal/workflow', 'test': 'TestLensPreviewLifecycleInstalledSchema', 'source': 'backend/internal/workflow/lens_preview_lifecycle_schema_integration_test.go', 'source_sha256': '266543f7eee362749df998ee136879ef9863dbfc2c4e30118b642d5459923801', 'head_requirement': ['lens_preview_lifecycle_schema_integration_test.go:17: isolated fully migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestMultiPreviewQuotaSQLPrivilegesAndTenantRefusal', 'source': 'backend/internal/workflow/multi_preview_quota_integration_test.go', 'source_sha256': '3071a62582083eb541337032850e8819729e223f83ca27cb54b1587de8f85a24', 'head_requirement': ['multi_preview_quota_integration_test.go:15: isolated migrated DB required']}, {'package': './internal/workflow', 'test': 'TestMultiPreviewWitnessGateIsPrivate', 'source': 'backend/internal/workflow/multi_preview_witness_integration_test.go', 'source_sha256': 'df6b3212786b7427e66a7db28968fecb58f7040618fbcc7085485f85a7bfdd9e', 'head_requirement': ['multi_preview_witness_integration_test.go:15: isolated migrated DB required']}, {'package': './internal/workflow', 'test': 'TestIntegrationMultiReplacementStartupCurrentInstallation', 'source': 'backend/internal/workflow/multi_replacement_startup_sql_integration_test.go', 'source_sha256': '1139ff36b34504806fabe7d8f8854916db08f07a5225de3504030801de537306', 'head_requirement': ['multi_replacement_startup_sql_integration_test.go:11: isolated fully migrated database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationNativeRecoveryInstallationRoles', 'source': 'backend/internal/workflow/native_recovery_installation_integration_test.go', 'source_sha256': '8d7cd258ccc2433b56b15ea1e30929b48df7573bfe3f301dcf1c834c1baaf48e', 'head_requirement': ['native_recovery_installation_integration_test.go:17: isolated PostgreSQL fixture required']}, {'package': './internal/workflow', 'test': 'TestIntegrationOpenEyesBeforeMediaRequiresExactDonorVersion', 'source': 'backend/internal/workflow/open_eyes_before_media_integration_test.go', 'source_sha256': 'aab8eed29692d82a65629e5ab2deb3b46dfe8e19bbd0b033f073e1e7b385a6d7', 'head_requirement': ['open_eyes_before_media_integration_test.go:17: isolated database migrated through 01957 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationPerspectiveLinesReceiptPrivateBoundaries', 'source': 'backend/internal/workflow/perspective_lines_receipt_installation_integration_test.go', 'source_sha256': '0c36931067d7d22c63d4245e1ed09e17d2d37a5d8ecf55a5bad37206fe3d3a33', 'head_requirement': ['perspective_lines_receipt_installation_integration_test.go:15: isolated PostgreSQL fixture required']}, {'package': './internal/workflow', 'test': 'TestIntegrationPerspectiveReplacementExecutionSourceInstallation', 'source': 'backend/internal/workflow/perspective_replacement_execution_source_sql_integration_test.go', 'source_sha256': '41f288f596b2010480e40104744d2ebf5b5a86e4c2a22f819433b02dfb6ae038', 'head_requirement': ['perspective_replacement_execution_source_sql_integration_test.go:13: isolated migrated database required']}, {'package': './internal/workflow', 'test': 'TestIntegrationRecovery', 'source': 'backend/internal/workflow/recovery_integration_test.go', 'source_sha256': 'd02011f86fee4c4e43d52235760c14ad4e826c2f2d04483330965a443ca0ae27', 'head_requirement': ['recovery_integration_test.go:17: isolated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestIntegrationDispatcherTriesNextNode', 'source': 'backend/internal/workflow/recovery_integration_test.go', 'source_sha256': 'd02011f86fee4c4e43d52235760c14ad4e826c2f2d04483330965a443ca0ae27', 'head_requirement': ['recovery_integration_test.go:128: isolated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestIntegrationRenderReviewCompletionReadinessAuthority', 'source': 'backend/internal/workflow/render_review_completion_readiness_integration_test.go', 'source_sha256': 'c291d00c332ca47c7f25a267054d367487b67d868313aab53161572e30ca49b9', 'head_requirement': ['render_review_completion_readiness_integration_test.go:16: isolated database migrated through 01790 required']}, {'package': './internal/workflow', 'test': 'TestIntegrationRenderSelectedReviewFinalizerAuthority', 'source': 'backend/internal/workflow/render_selected_review_finalization_integration_test.go', 'source_sha256': '93b1e9e4543dd2a531e1469cfdc434b9eb9522e79649ee4dd59e6c883efc7462', 'head_requirement': ['render_selected_review_finalization_integration_test.go:15: isolated database migrated through 01791 required']}, {'package': './internal/workflow', 'test': 'TestRestorationPreviewDraftParamsRetainedBeforeTaskInputs', 'source': 'backend/internal/workflow/restoration_preview_gc_integration_test.go', 'source_sha256': '8922945b263d0bc397f080029b1c00946091e033716759f07c87778aa768d25d', 'head_requirement': ['restoration_preview_gc_integration_test.go:20: isolated migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestV29NegativeGCMatcherRefusesSubstitutionAndMalformedDraft', 'source': 'backend/internal/workflow/restoration_preview_gc_integration_test.go', 'source_sha256': '8922945b263d0bc397f080029b1c00946091e033716759f07c87778aa768d25d', 'head_requirement': ['restoration_preview_gc_integration_test.go:42: isolated migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestV29ReplacementGCMatcherCoversSavedDraftForms', 'source': 'backend/internal/workflow/restoration_preview_gc_integration_test.go', 'source_sha256': '8922945b263d0bc397f080029b1c00946091e033716759f07c87778aa768d25d', 'head_requirement': ['restoration_preview_gc_integration_test.go:90: isolated migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestV28DraftParamsReferenceRequiresSavedOwnerAndDigest', 'source': 'backend/internal/workflow/restoration_preview_gc_integration_test.go', 'source_sha256': '8922945b263d0bc397f080029b1c00946091e033716759f07c87778aa768d25d', 'head_requirement': ['restoration_preview_gc_integration_test.go:146: isolated migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestV29DraftParamsReferenceUsesMainPlanAndPreviewScope', 'source': 'backend/internal/workflow/restoration_preview_gc_integration_test.go', 'source_sha256': '8922945b263d0bc397f080029b1c00946091e033716759f07c87778aa768d25d', 'head_requirement': ['restoration_preview_gc_integration_test.go:178: isolated migrated PostgreSQL required']}, {'package': './internal/workflow', 'test': 'TestV30RuntimeStartupRequiresCurrentAdmissionABI', 'source': 'backend/internal/workflow/v30_runtime_startup_sql_integration_test.go', 'source_sha256': 'd059d633b20034dc07d7525deb0df060069dc1b11889ceb64c8f4991aac82364', 'head_requirement': ['v30_runtime_startup_sql_integration_test.go:16: isolated database migrated through V30 runtime gates required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationReadApprovalDraftPinsCurrentDocument', 'source': 'backend/internal/v2draft/approval_read_integration_test.go', 'source_sha256': 'c4fcc4c8a20c985b0c31f22335e65cd9b31dd897c2e2957c4d493c36625494c7', 'head_requirement': ['approval_read_integration_test.go:69: isolated PostgreSQL with V21 migration required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationPortableBackgroundAsset', 'source': 'backend/internal/v2draft/background_asset_integration_test.go', 'source_sha256': 'e565ad0e9fbcf099c56f8a235196b7ad4d91a57db553945fad27628193b50c04', 'head_requirement': ['background_asset_integration_test.go:20: isolated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationGlareSourceAuthorityIsolation', 'source': 'backend/internal/v2draft/glare_source_sql_integration_test.go', 'source_sha256': 'de9b01e35fbe9ba26fcc009c26257718d772d9456e5c7524cb226d3986f03274', 'head_requirement': ['glare_source_sql_integration_test.go:16: isolated database through02480 required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityAdoptionQueueIsolation', 'source': 'backend/internal/v2draft/identity_adoption_queue_integration_test.go', 'source_sha256': 'ebf4f80ccae0f583019dfce06aa65748133bab52a452782026c2bab65d72b02f', 'head_requirement': ['identity_adoption_queue_integration_test.go:16: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityDispatchRequiresAdopterAndQualification', 'source': 'backend/internal/v2draft/identity_dispatch_activation_integration_test.go', 'source_sha256': 'f861afafca1ca2d8f3b1966f83530fccfd14b9802046b3eada013053a7eb9216', 'head_requirement': ['identity_dispatch_activation_integration_test.go:16: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityResultObjectRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:28: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityResultInputRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:90: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityConsentInputRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:125: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityLiveConsentGateRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:157: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityLiveSourceGateRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:190: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityLiveQualificationGateRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:223: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityCalibrationInputRoleScope', 'source': 'backend/internal/v2draft/identity_result_catalog_test.go', 'source_sha256': '58d345fd719396ed0010f7464032f1d9aced289ffb93058d99794912fc514cd3', 'head_requirement': ['identity_result_catalog_test.go:257: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationIdentityVerifiedAdoptionFunctionAndRoles', 'source': 'backend/internal/v2draft/identity_verified_adoption_integration_test.go', 'source_sha256': 'a179d6849908759c20e7b6233f15fa2451caeec951de6d14b89ab7496bde1fe4', 'head_requirement': ['identity_verified_adoption_integration_test.go:16: isolated migrated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestLocalPortraitSQLInventoryDigestParity', 'source': 'backend/internal/v2draft/local_portrait_digest_integration_test.go', 'source_sha256': 'e51731f3e8fde9cd97ce3d8b92172640e5926941cf956eb69d7813da18e23803', 'head_requirement': ['local_portrait_digest_integration_test.go:18: PHOTO_TEST_DATABASE_URL with owner-role access and migration 00835 required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationReadyDraftVersionReaderIsPrivateAndScopeBound', 'source': 'backend/internal/v2draft/publisher_ready_version_integration_test.go', 'source_sha256': 'b1d31dcb15119f1a61d10d247cffbd76c5640086378f6de1075dbfa957df89bb', 'head_requirement': ['publisher_ready_version_integration_test.go:16: isolated PostgreSQL with migration 01888 required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationRenderConsentDiagnosticUnknownScopeAndACL', 'source': 'backend/internal/v2draft/render_consent_diagnostic_integration_test.go', 'source_sha256': 'c036e05dddac9c018cdfdbd22a7a0857880e147b363240e80c8efbc3b02d5bb7', 'head_requirement': ['render_consent_diagnostic_integration_test.go:17: isolated PostgreSQL required']}, {'package': './internal/v2draft', 'test': 'TestIntegrationShotListCASReplayAndRevokedConsent', 'source': 'backend/internal/v2draft/shot_list_integration_test.go', 'source_sha256': 'a61d57c0630119aef6a73db3fa204f06c529cb11070b6c72e080e73813ae91af', 'head_requirement': ['shot_list_integration_test.go:20: isolated PostgreSQL with V24 migrations required']}]

def _output_text(value):
    if value is None:
        return ''
    if isinstance(value, bytes):
        return value.decode('utf-8', errors='replace')
    if isinstance(value, str):
        return value
    raise TypeError('Unexpected subprocess output type')


@pytest.mark.parametrize('case', CASES, ids=[c['test'] for c in CASES])
def test_each_go_root_in_its_own_private_database(isolated_database_config, case):
    root=Path(__file__).resolve().parents[2]
    source_sha=hashlib.sha256((root/case['source']).read_bytes()).hexdigest()
    assert source_sha==case['source_sha256'], 'reviewed Go source changed; refresh plan'
    name=isolated_database_config['dbname']
    assert isinstance(name,str) and re.fullmatch(r'photo_test_[0-9a-f]{32}',name), 'private DB name required'
    # This query is authored only; it is not executed during source review.
    # Use the owning fixture configuration, never an ambient/shared test DSN.
    with psycopg.connect(**isolated_database_config) as connection:
        assert connection.info.dbname==name
        assert connection.execute('SELECT current_database()').fetchone()==(name,)
    # Strip ALL specialized PHOTO_* inputs, including PHOTO_RENDER_*,
    # PHOTO_BATCH_*, PHOTO_SEMANTIC_* and PHOTO_WORKER_PYTHON.
    # This test intentionally exercises only the closed DSN-only case list.
    env={k:v for k,v in os.environ.items() if not k.startswith('PHOTO_')}
    env.update(PHOTO_TEST_DATABASE_URL=make_conninfo(**isolated_database_config),
               GOCACHE=str(root/'work/go-cache'),GOMODCACHE=str(root/'.cache/go-mod'),GOPROXY='off',GOTOOLCHAIN='local',GOMAXPROCS='2')
    command=['go','test','-json','-p','1',case['package'],'-run','^'+case['test']+'$','-count=1','-timeout=60s']
    report=root/'work/go69-isolated-results'/case['test']/uuid4().hex
    report.mkdir(parents=True)
    metadata=dict(command=command,selector=case['test'],package=case['package'],
                  source=case['source'],source_sha256=source_sha,
                  expected_source_sha256=case['source_sha256'],
                  timeout_seconds=90,go_timeout_seconds=60)
    phases=[]
    stdout=stderr=''
    timed_out=False
    cleanup_complete=True
    process=None
    owned_pgid=None
    launch_error=None
    def remember_timeout(error):
        nonlocal stdout,stderr
        # communicate retries return cumulative output; do not concatenate it.
        if error.stdout is not None:stdout=_output_text(error.stdout)
        if error.stderr is not None:stderr=_output_text(error.stderr)
    def signal_owned_group(sig,phase):
        nonlocal cleanup_complete
        assert owned_pgid==process.pid and owned_pgid>1
        try:
            os.killpg(owned_pgid,sig)
        except ProcessLookupError:
            phases.append(dict(phase=phase,signal=sig.name,outcome='group_absent'))
        except OSError as error:
            cleanup_complete=False
            phases.append(dict(phase=phase,signal=sig.name,outcome='signal_error',errno=error.errno))
        else:
            phases.append(dict(phase=phase,signal=sig.name,outcome='sent'))
    interrupted = None
    def cleanup_after_interrupt(original):
        nonlocal stdout, stderr, cleanup_complete
        # Keep the first exception; a second Ctrl-C/cleanup error must not skip
        # the kill attempt or replace the original pytest interruption.
        def record_cleanup_error(phase, error):
            nonlocal cleanup_complete
            cleanup_complete=False
            phases.append(dict(phase=phase,outcome='cleanup_error',error_type=type(error).__name__))
            original.add_note('Owned Go group cleanup error during '+phase+': '+type(error).__name__)
        try:
            signal_owned_group(signal.SIGINT,'cancel_interrupt_owned_group')
            out,err=process.communicate(timeout=5)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='cancel_interrupt_grace',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            remember_timeout(error)
            phases.append(dict(phase='cancel_interrupt_grace',outcome='timeout',seconds=5))
        except BaseException as error:
            record_cleanup_error('cancel_interrupt_grace',error)
        finally:
            try:
                # Descendants may survive a completed group leader.
                signal_owned_group(signal.SIGKILL,'cancel_kill_owned_group')
            except BaseException as error:
                record_cleanup_error('cancel_kill_owned_group',error)
        try:
            out,err=process.communicate(timeout=5)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='cancel_kill_drain',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            remember_timeout(error)
            cleanup_complete=False
            phases.append(dict(phase='cancel_kill_drain',outcome='timeout',seconds=5))
        except BaseException as error:
            record_cleanup_error('cancel_kill_drain',error)
        # Never leave evidence collection waiting on a descendant-held pipe.
        for pipe in (process.stdout,process.stderr):
            if pipe is not None:
                try: pipe.close()
                except BaseException as error: record_cleanup_error('cancel_close_pipe',error)
        deadline=time.monotonic()+5
        while True:
            try:
                os.killpg(owned_pgid,0)
            except ProcessLookupError:
                phases.append(dict(phase='cancel_group_absence_check',outcome='absent'))
                break
            except BaseException as error:
                record_cleanup_error('cancel_group_absence_check',error)
                break
            if time.monotonic()>=deadline:
                cleanup_complete=False
                phases.append(dict(phase='cancel_group_absence_check',outcome='still_present',seconds=5))
                break
            try: time.sleep(.05)
            except BaseException as error:
                record_cleanup_error('cancel_group_absence_wait',error)
                break
    try:
        # Defer Python SIGINT only while ownership is being established. Do not
        # block OS signals or change the child's inherited mask/preexec state.
        previous_sigint=signal.getsignal(signal.SIGINT)
        pending_sigint=[]
        def defer_sigint(signum, frame):
            if previous_sigint != signal.SIG_IGN and not pending_sigint:
                pending_sigint.append((signum,frame))
        initialization_error=None
        signal.signal(signal.SIGINT,defer_sigint)
        try:
            try:
                process=subprocess.Popen(command,cwd=root/'backend',env=env,
                    stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
            except OSError as error:
                launch_error=error
                metadata.update(launch_error_type=type(error).__name__,launch_errno=error.errno)
                phases.append(dict(phase='launch',outcome='failed'))
            if process is not None:
                # All cleanup functions already exist before the child starts.
                owned_pgid=process.pid
                metadata.update(pid=process.pid,owned_pgid=owned_pgid,
                    interrupt_grace_seconds=5,kill_drain_seconds=5)
        except BaseException as error:
            initialization_error=error
        finally:
            signal.signal(signal.SIGINT,previous_sigint)
        if pending_sigint:
            signum,frame=pending_sigint[0]
            handler=previous_sigint if callable(previous_sigint) else signal.default_int_handler
            handler(signum,frame)
        if initialization_error is not None:
            raise initialization_error
        if launch_error is not None:
            raise launch_error
        try:
            out,err=process.communicate(timeout=90)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='initial_wait',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            timed_out=True
            remember_timeout(error)
            phases.append(dict(phase='initial_wait',outcome='timeout',seconds=90))
            signal_owned_group(signal.SIGINT,'interrupt_owned_group')
            try:
                out,err=process.communicate(timeout=5)
                stdout,stderr=_output_text(out),_output_text(err)
                phases.append(dict(phase='interrupt_grace',outcome='completed',returncode=process.returncode))
            except subprocess.TimeoutExpired as error:
                remember_timeout(error)
                phases.append(dict(phase='interrupt_grace',outcome='timeout',seconds=5))
            # Also kill remaining descendants when the leader exited during grace.
            # ESRCH is an expected successful no-op for an already absent group.
            signal_owned_group(signal.SIGKILL,'kill_owned_group')
            try:
                out,err=process.communicate(timeout=5)
                stdout,stderr=_output_text(out),_output_text(err)
                phases.append(dict(phase='kill_drain',outcome='completed',returncode=process.returncode))
            except subprocess.TimeoutExpired as error:
                remember_timeout(error)
                cleanup_complete=False
                phases.append(dict(phase='kill_drain',outcome='timeout',seconds=5,
                                   returncode=process.poll()))
                # Never hang evidence collection on a descendant-held pipe.
                for pipe in (process.stdout,process.stderr):
                    if pipe is not None:pipe.close()
            deadline=time.monotonic()+5
            while True:
                try:
                    os.killpg(owned_pgid,0)
                except ProcessLookupError:
                    phases.append(dict(phase='group_absence_check',outcome='absent'))
                    break
                except OSError as error:
                    cleanup_complete=False
                    phases.append(dict(phase='group_absence_check',outcome='probe_error',errno=error.errno))
                    break
                if time.monotonic()>=deadline:
                    cleanup_complete=False
                    phases.append(dict(phase='group_absence_check',outcome='still_present',seconds=5))
                    break
                time.sleep(.05)
    except BaseException as error:
        interrupted=error
        phases.append(dict(phase='runner_interrupted',outcome='interrupted',error_type=type(error).__name__))
        if process is not None:
            try:
                if owned_pgid is None:owned_pgid=process.pid
                cleanup_after_interrupt(error)
            except BaseException as cleanup_error:
                cleanup_complete=False
                error.add_note('Owned Go cleanup unexpectedly failed: '+type(cleanup_error).__name__)
        raise
    finally:
        try:
            (report/'go.jsonl').write_text(stdout)
            (report/'stderr.log').write_text(stderr)
            (report/'result.json').write_text(json.dumps(dict(metadata,
                status='launch_error' if interrupted is launch_error and launch_error is not None else ('interrupted' if interrupted is not None else ('timeout' if timed_out else 'completed')),timed_out=timed_out,
                interruption_type=type(interrupted).__name__ if interrupted is not None else None,
                returncode=process.poll() if process is not None else None,
                process_group_cleanup_status='not_started' if process is None else (('verified_absent' if cleanup_complete else 'incomplete') if (timed_out or interrupted is not None) else 'descendant_cleanup_not_verified'),
                pg_session_cleanup_status='outer_teardown_postflight_required',
                phases=phases),indent=2)+'\n')
        except BaseException as evidence_error:
            if interrupted is None:
                raise
            interrupted.add_note('Go evidence write failed: '+type(evidence_error).__name__)
    if timed_out:
        pytest.fail('Go selector timed out; owned group cleanup and partial evidence saved: '+str(report),pytrace=False)
    assert process.returncode==0, stdout+stderr
    events=[json.loads(line) for line in stdout.splitlines() if line.startswith('{')]
    assert not [x for x in events if x.get('Action') in ('skip','fail')], 'nested SKIP/failure is not execution evidence'
    assert any(x.get('Action')=='pass' and x.get('Test')==case['test'] for x in events), 'exact root PASS required'
