CREATE SCHEMA IF NOT EXISTS raw;

CREATE TABLE IF NOT EXISTS raw.project (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS raw.author (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS raw.issue (
    id BIGINT PRIMARY KEY,
    project_id BIGINT REFERENCES raw.project(id),
    author_id BIGINT,
    assignee_id BIGINT,
    key TEXT NOT NULL UNIQUE,
    summary TEXT,
    description TEXT,
    type TEXT,
    priority TEXT,
    status TEXT,
    created_time TIMESTAMPTZ,
    closed_time TIMESTAMPTZ,
    updated_time TIMESTAMPTZ,
    time_spent INTEGER,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS raw.status_changes (
    id SERIAL PRIMARY KEY,
    issue_id BIGINT REFERENCES raw.issue(id),
    author_id BIGINT REFERENCES raw.author(id),
    change_time TIMESTAMPTZ NOT NULL,
    from_status TEXT,
    to_status TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_issue_project_id ON raw.issue(project_id);
CREATE INDEX IF NOT EXISTS idx_issue_status ON raw.issue(status);
CREATE INDEX IF NOT EXISTS idx_status_changes_issue_id ON raw.status_changes(issue_id);
