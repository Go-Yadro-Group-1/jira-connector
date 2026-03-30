package consumer

type Consumer struct{}

func New() (*Consumer, error) {
	return &Consumer{}, nil
}
