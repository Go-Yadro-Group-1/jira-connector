INSERT INTO raw.issue (
    id, project_id, author_id, assignee_id, key,
    summary, description, type, priority, status,
    created_time, closed_time, updated_time, time_spent
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (id) DO UPDATE SET
    project_id   = EXCLUDED.project_id,
    author_id    = EXCLUDED.author_id,
    assignee_id  = EXCLUDED.assignee_id,
    key          = EXCLUDED.key,
    summary      = EXCLUDED.summary,
    description  = EXCLUDED.description,
    type         = EXCLUDED.type,
    priority     = EXCLUDED.priority,
    status       = EXCLUDED.status,
    created_time = EXCLUDED.created_time,
    closed_time  = EXCLUDED.closed_time,
    updated_time = EXCLUDED.updated_time,
    time_spent   = EXCLUDED.time_spent;
