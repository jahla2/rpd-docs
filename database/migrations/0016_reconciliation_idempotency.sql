BEGIN;

CREATE UNIQUE INDEX IF NOT EXISTS ux_matches_run_source_target
    ON matches(tenant_id,reconciliation_run_id,source_record_id,target_record_id);

CREATE UNIQUE INDEX IF NOT EXISTS ux_reconciliation_exceptions_run_source_type
    ON reconciliation_exceptions(tenant_id,reconciliation_run_id,source_record_id,exception_type);

CREATE UNIQUE INDEX IF NOT EXISTS ux_review_tasks_open_source
    ON review_tasks(tenant_id,reconciliation_run_id,source_record_id,task_type)
    WHERE status='OPEN' AND reconciliation_run_id IS NOT NULL AND source_record_id IS NOT NULL;

COMMIT;
