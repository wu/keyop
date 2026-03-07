package util

import (
	"sync"
	"time"
)

// RateLimiter implements a simple fixed-window sliding counter using N buckets.
// It keeps track of events in a rolling window divided into equal-duration buckets.
// Defaults to a 60s window with 10 buckets (6s each) when created with NewRateLimiter.
type RateLimiter struct {
	mu             sync.Mutex
	buckets        []int
	current        int
	start          time.Time
	bucketDuration time.Duration
	bucketCount    int
	limit          int
	// number of dropped events since the last successful allowed event
	droppedSinceWarning int
}

// NewRateLimiter creates a RateLimiter using a 60s window split into 10 buckets.
func NewRateLimiter(limit int) *RateLimiter {
	return NewRateLimiterConfig(limit, 60*time.Second, 10)
}

// NewRateLimiterConfig creates a RateLimiter with a custom window and bucket count.
func NewRateLimiterConfig(limit int, window time.Duration, buckets int) *RateLimiter {
	if buckets <= 0 {
		buckets = 10
	}
	if window <= 0 {
		window = 60 * time.Second
	}
	bd := window / time.Duration(buckets)
	if bd <= 0 {
		bd = time.Second
	}
	return &RateLimiter{
		buckets:        make([]int, buckets),
		current:        0,
		start:          time.Time{},
		bucketDuration: bd,
		bucketCount:    buckets,
		limit:          limit,
	}
}

// AddEventAt records an event occurring at the provided time and returns whether the
// event is allowed (not exceeding the configured limit) and whether this event is the
// first dropped event since the last successful allowed event.
func (r *RateLimiter) AddEventAt(now time.Time) (allowed bool, firstDrop bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.start.IsZero() {
		r.start = now
		r.current = 0
	}

	// advance buckets according to elapsed time
	elapsed := now.Sub(r.start)
	steps := int(elapsed / r.bucketDuration)
	if steps > 0 {
		if steps >= r.bucketCount {
			// too much time passed, clear all
			for i := range r.buckets {
				r.buckets[i] = 0
			}
			r.current = 0
			r.start = now
		} else {
			for i := 0; i < steps; i++ {
				r.current = (r.current + 1) % r.bucketCount
				r.buckets[r.current] = 0
			}
			r.start = r.start.Add(time.Duration(steps) * r.bucketDuration)
		}
	}

	// record this event
	r.buckets[r.current]++

	// compute total in window
	total := 0
	for _, v := range r.buckets {
		total += v
	}

	if total > r.limit {
		r.droppedSinceWarning++
		first := r.droppedSinceWarning == 1
		return false, first
	}

	// allowed -- reset dropped counter
	if r.droppedSinceWarning > 0 {
		r.droppedSinceWarning = 0
	}

	return true, false
}

// AddEvent records an event occurring now and returns allowed, firstDrop.
func (r *RateLimiter) AddEvent() (bool, bool) {
	return r.AddEventAt(time.Now())
}

// SetLimit updates the allowed limit per window.
func (r *RateLimiter) SetLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit = limit
}

// Total returns the current total event count in the window.
func (r *RateLimiter) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, v := range r.buckets {
		total += v
	}
	return total
}
