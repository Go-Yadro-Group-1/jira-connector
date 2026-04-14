package raw

import "time"

type Project struct {
	ID    int64
	Title string
}

type Author struct {
	ID   int64
	Name string
}

type Issue struct {
	ID          int64
	ProjectID   int64
	AuthorID    int64
	AssigneeID  *int64
	Key         string
	Summary     *string
	Description *string
	Type        *string
	Priority    *string
	Status      *string
	CreatedTime *time.Time
	ClosedTime  *time.Time
	UpdatedTime *time.Time
	TimeSpent   *int
}

type StatusChange struct {
	IssueID    int64
	AuthorID   int64
	ChangeTime time.Time
	FromStatus *string
	ToStatus   *string
}
