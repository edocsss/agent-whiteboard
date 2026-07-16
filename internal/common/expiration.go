package common

import (
	"math"
	"time"
)

func expirationAt(now time.Time, seconds int64) (*time.Time, error) {
	if seconds < 0 {
		return nil, NewError(CodeInvalidRequest, "expiration must not be negative", nil)
	}
	if seconds == 0 {
		return nil, nil
	}
	unix := now.Unix()
	if unix > 0 && seconds > math.MaxInt64-unix {
		return nil, NewError(CodeInvalidRequest, "expiration overflows unix time", nil)
	}
	value := time.Unix(unix+seconds, 0).UTC()
	return &value, nil
}

func ResolveCreateExpiration(now time.Time, defaultSeconds int64, supplied *int64) (*time.Time, error) {
	if supplied == nil {
		return expirationAt(now, defaultSeconds)
	}
	return expirationAt(now, *supplied)
}

func ResolveUpdateExpiration(now time.Time, current *time.Time, supplied *int64) (*time.Time, error) {
	if supplied == nil {
		return current, nil
	}
	return expirationAt(now, *supplied)
}

func IsExpired(now time.Time, expiresAt *time.Time) bool {
	return expiresAt != nil && !now.Before(*expiresAt)
}
