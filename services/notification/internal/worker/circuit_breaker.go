package worker

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

type CircuitBreakerState int

const (
	CircuitBreakerStateClosed CircuitBreakerState = iota
	CircuitBreakerStateOpen
	CircuitBreakerStateHalfOpen
)

type CircuitBreaker struct {
	state           CircuitBreakerState
	failureCount    int
	openDuration    time.Duration // Time to stay open before transitioning to half-open
	lastFailureTime time.Time
	mu              sync.Mutex

	maxFailures int
}

func NewCircuitBreaker(maxFailures int, openDuration time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:           CircuitBreakerStateClosed,
		failureCount:    0,
		openDuration:    openDuration,
		lastFailureTime: time.Time{},
		mu:              sync.Mutex{},
		maxFailures:     maxFailures,
	}
}

func (cb *CircuitBreaker) Execute(operation func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitBreakerStateOpen:
		if time.Since(cb.lastFailureTime) > cb.openDuration {
			cb.state = CircuitBreakerStateHalfOpen
		} else {
			return ErrCircuitBreakerOpen
		}
	case CircuitBreakerStateHalfOpen:
		err := operation()
		if err != nil {
			cb.state = CircuitBreakerStateOpen
			cb.lastFailureTime = time.Now()
			return err
		}
		cb.state = CircuitBreakerStateClosed
		cb.failureCount = 0
		return nil
	case CircuitBreakerStateClosed:
		err := operation()
		if err != nil {
			cb.failureCount++
			if cb.failureCount >= cb.maxFailures {
				cb.state = CircuitBreakerStateOpen
				cb.lastFailureTime = time.Now()
			}
			return err
		}
		cb.failureCount = 0
		return nil
	}
	return nil
}
