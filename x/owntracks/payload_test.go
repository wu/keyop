package owntracks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocationEventPayloadType(t *testing.T) {
	e := LocationEvent{}
	assert.Equal(t, "service.owntracks.location.v1", e.PayloadType())
}

func TestLocationEnterEventPayloadType(t *testing.T) {
	e := LocationEnterEvent{}
	assert.Equal(t, "service.owntracks.enter.v1", e.PayloadType())
}

func TestLocationExitEventPayloadType(t *testing.T) {
	e := LocationExitEvent{}
	assert.Equal(t, "service.owntracks.exit.v1", e.PayloadType())
}
