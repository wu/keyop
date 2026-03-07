package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_AllowsUntilLimitAndDrops(t *testing.T) {
	limit := 5
	r := NewRateLimiter(limit)
	base := time.Now()

	// add limit events - should be allowed
	for i := 0; i < limit; i++ {
		allowed, firstDrop := r.AddEventAt(base)
		assert.True(t, allowed, "expected allowed for event %d", i+1)
		assert.False(t, firstDrop, "firstDrop should be false for allowed events")
	}

	// next event should be dropped and firstDrop=true
	allowed, firstDrop := r.AddEventAt(base)
	assert.False(t, allowed, "expected dropped after exceeding limit")
	assert.True(t, firstDrop, "expected firstDrop to be true on first drop")

	// another dropped event should have firstDrop=false
	allowed, firstDrop = r.AddEventAt(base)
	assert.False(t, allowed)
	assert.False(t, firstDrop, "expected subsequent drops to return firstDrop=false")

	// total should reflect all recorded events in window
	total := r.Total()
	assert.Equal(t, limit+2, total)
}

func TestRateLimiter_WindowResetClearsCounts(t *testing.T) {
	// small window and buckets for deterministic rotation
	r := NewRateLimiterConfig(1, 6*time.Second, 3) // 3 buckets, 2s each
	base := time.Now()

	// first event allowed
	allowed, _ := r.AddEventAt(base)
	assert.True(t, allowed)
	assert.Equal(t, 1, r.Total())

	// advance time beyond the whole window -> should clear buckets
	later := base.Add(7 * time.Second)
	allowed, _ = r.AddEventAt(later)
	assert.True(t, allowed, "expected allowed after window expired and counts cleared")
	assert.Equal(t, 1, r.Total(), "total should be 1 after window reset and single event")
}

func TestRateLimiter_SetLimitAllowsMoreEventsAndResetsDropState(t *testing.T) {
	r := NewRateLimiter(2)
	base := time.Now()

	// hit the limit
	for i := 0; i < 2; i++ {
		allowed, _ := r.AddEventAt(base)
		assert.True(t, allowed)
	}

	// cause a drop
	allowed, firstDrop := r.AddEventAt(base)
	assert.False(t, allowed)
	assert.True(t, firstDrop)

	// increase the limit so subsequent events are allowed
	r.SetLimit(5)
	allowed, _ = r.AddEventAt(base)
	assert.True(t, allowed, "expected allowed after increasing the limit")
}

func TestRateLimiter_BucketRotationAccumulatesAcrossBuckets(t *testing.T) {
	// window=6s, buckets=3 -> bucketDuration=2s
	r := NewRateLimiterConfig(10, 6*time.Second, 3)
	base := time.Now()

	// two events in first bucket
	r.AddEventAt(base)
	r.AddEventAt(base)
	assert.Equal(t, 2, r.Total())

	// advance by 3s -> moves forward by 1 bucket (2s step), leaving previous bucket counts
	mid := base.Add(3 * time.Second)
	r.AddEventAt(mid)
	assert.Equal(t, 3, r.Total())
}
