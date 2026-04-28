DROP INDEX IF EXISTS idx_status_changes_issue_id;
DROP INDEX IF EXISTS idx_issue_status;
DROP INDEX IF EXISTS idx_issue_project_id;

DROP TABLE IF EXISTS raw.status_changes;
DROP TABLE IF EXISTS raw.issue;
DROP TABLE IF EXISTS raw.author;
DROP TABLE IF EXISTS raw.project;
