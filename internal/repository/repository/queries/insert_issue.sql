INSERT INTO raw.issue (
    id,
    project_id,
    author_id,
    assignee_id,
    key,
    summary,
    description,
    type,
    priority,
    status,
    created_time,
    closed_time,
    updated_time,
    time_spent
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
);