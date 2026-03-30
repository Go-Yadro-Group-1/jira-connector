package publisher

type Publisher struct{}

func New() (*Publisher, error) {
	return &Publisher{}, nil
}
