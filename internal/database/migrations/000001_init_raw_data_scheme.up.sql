CREATE SCHEMA IF NOT EXISTS raw;

CREATE TABLE raw.project (
    id INT PRIMARY KEY,
    title TEXT NOT NULL
);

CREATE TABLE raw.author (
    id INT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE raw.issue (
    id INT PRIMARY KEY,
    project_id INT NOT NULL REFERENCES raw.project(id),
    author_id INT NOT NULL REFERENCES raw.author(id),
    assignee_id INT REFERENCES raw.author(id),
    key TEXT NOT NULL,
    summary TEXT,
    description TEXT,
    type TEXT,
    priority TEXT,
    status TEXT,
    created_time TIMESTAMP,
    closed_time TIMESTAMP,
    updated_time TIMESTAMP,
    time_spent INT
);

CREATE TABLE raw.status_changes (
    issue_id INT NOT NULL REFERENCES raw.issue(id),
    author_id INT NOT NULL REFERENCES raw.author(id),
    change_time TIMESTAMP NOT NULL,
    from_status TEXT,
    to_status TEXT
);
