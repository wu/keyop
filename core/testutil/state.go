package testutil

import "github.com/wu/keyop/core"

// NoOpStateStore is a StateStoreApi that performs no persistence (useful for tests).
type NoOpStateStore struct{}

func (s *NoOpStateStore) Save(_ string, _ interface{}) error { return nil }
func (s *NoOpStateStore) Load(_ string, _ interface{}) error { return nil }

// Compile-time check that *NoOpStateStore satisfies core.StateStoreApi.
var _ core.StateStoreApi = (*NoOpStateStore)(nil)
