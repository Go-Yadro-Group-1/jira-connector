package raw

import "time"

type Project struct {
	ID    int
	Title string
}

type Author struct {
	ID   int
	Name string
}

type Issue struct {
	ID          int
	ProjectID   int
	AuthorID    int
	AssigneeID  *int
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
	IssueID    int
	AuthorID   int
	ChangeTime time.Time
	FromStatus *string
	ToStatus   *string
}
