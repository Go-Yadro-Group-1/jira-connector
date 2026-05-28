CREATE SCHEMA IF NOT EXISTS analytics;

CREATE TABLE analytics.open_task_time (
    id_project INT NOT NULL REFERENCES raw.project(id),
    creation_time TIMESTAMP NOT NULL,
    data JSONB NOT NULL,
    UNIQUE(id_project)
);

CREATE TABLE analytics.task_state_time (
    id_project INT NOT NULL REFERENCES raw.project(id),
    creation_time TIMESTAMP NOT NULL,
    data JSONB NOT NULL,
    state TEXT NOT NULL,
    UNIQUE(id_project, state)
);

CREATE TABLE analytics.complexity_task_time (
    id_project INT NOT NULL REFERENCES raw.project(id),
    creation_time TIMESTAMP NOT NULL,
    data JSONB NOT NULL,
    UNIQUE(id_project)
);

CREATE TABLE analytics.task_priority_count (
    id_project INT NOT NULL REFERENCES raw.project(id),
    creation_time TIMESTAMP NOT NULL,
    state TEXT NOT NULL,
    data JSONB NOT NULL,
    UNIQUE(id_project, state)
);

CREATE TABLE analytics.activity_by_task (
    id_project INT NOT NULL REFERENCES raw.project(id),
    creation_time TIMESTAMP NOT NULL,
    state TEXT NOT NULL,
    data JSONB NOT NULL,
    UNIQUE(id_project, state)
);
