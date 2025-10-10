package core

import "context"

type Dependencies struct {
	Logger   Logger
	Hostname string
	Context  context.Context
}
