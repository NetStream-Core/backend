package storage

type Storage interface {
	Write(data any) error
	Close() error
}
