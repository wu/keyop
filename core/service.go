package core

type Service interface {
	Check() error
}
