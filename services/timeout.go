package services

import "time"

const defaultServiceTimeout = 2 * time.Second

func resolveTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultServiceTimeout
	}
	return timeout
}
