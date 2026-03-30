package postgres

//nolint:revive
type PostgresRepository struct{}

func New() (*PostgresRepository, error) {
	return &PostgresRepository{}, nil
}
