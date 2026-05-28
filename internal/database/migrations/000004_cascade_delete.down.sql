-- Revert analytics cascade constraints
ALTER TABLE analytics.activity_by_task
    DROP CONSTRAINT activity_by_task_id_project_fkey,
    ADD CONSTRAINT activity_by_task_id_project_fkey
        FOREIGN KEY (id_project) REFERENCES raw.project(id);

ALTER TABLE analytics.task_priority_count
    DROP CONSTRAINT task_priority_count_id_project_fkey,
    ADD CONSTRAINT task_priority_count_id_project_fkey
        FOREIGN KEY (id_project) REFERENCES raw.project(id);

ALTER TABLE analytics.complexity_task_time
    DROP CONSTRAINT complexity_task_time_id_project_fkey,
    ADD CONSTRAINT complexity_task_time_id_project_fkey
        FOREIGN KEY (id_project) REFERENCES raw.project(id);

ALTER TABLE analytics.task_state_time
    DROP CONSTRAINT task_state_time_id_project_fkey,
    ADD CONSTRAINT task_state_time_id_project_fkey
        FOREIGN KEY (id_project) REFERENCES raw.project(id);

ALTER TABLE analytics.open_task_time
    DROP CONSTRAINT open_task_time_id_project_fkey,
    ADD CONSTRAINT open_task_time_id_project_fkey
        FOREIGN KEY (id_project) REFERENCES raw.project(id);

-- Revert raw cascade constraints
ALTER TABLE raw.issue
    DROP CONSTRAINT issue_project_id_fkey,
    ADD CONSTRAINT issue_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES raw.project(id);

ALTER TABLE raw.status_changes
    DROP CONSTRAINT status_changes_issue_id_fkey,
    ADD CONSTRAINT status_changes_issue_id_fkey
        FOREIGN KEY (issue_id) REFERENCES raw.issue(id);
