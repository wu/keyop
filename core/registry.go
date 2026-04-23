package core

import "context"

// ServiceConstructor is the function signature all service packages must satisfy.
// It returns interface{} to accommodate service NewService functions that return
// concrete types (e.g., func() *Service) rather than the Service interface directly.
type ServiceConstructor func(deps Dependencies, cfg ServiceConfig, ctx context.Context) interface{}

var serviceRegistry = map[string]ServiceConstructor{}

// RegisterService registers a service constructor under typeName.
// Called from package init() functions; not safe for concurrent use.
func RegisterService(typeName string, constructor ServiceConstructor) {
	serviceRegistry[typeName] = constructor
}

// LookupService returns the constructor for typeName.
func LookupService(typeName string) (ServiceConstructor, bool) {
	c, ok := serviceRegistry[typeName]
	return c, ok
}
