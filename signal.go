package tunnel

type Signal interface {
	SendSignal(detail *NATDetail) error
	ReadSignal() (*NATDetail, error)
}
