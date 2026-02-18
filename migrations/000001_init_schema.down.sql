DROP TABLE IF EXISTS wvs.audit;
DROP TABLE IF EXISTS wvs.tasks;
ALTER TABLE wvs.workspaces DROP CONSTRAINT IF EXISTS fk_workspaces_current_snapshot;
DROP TABLE IF EXISTS wvs.snapshots;
DROP TABLE IF EXISTS wvs.workspaces;
DROP SCHEMA IF EXISTS wvs;
