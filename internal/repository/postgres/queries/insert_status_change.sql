INSERT INTO raw.status_changes (
    issue_id,
    author_id,
    change_time,
    from_status,
    to_status
)
VALUES ($1, $2, $3, $4, $5);