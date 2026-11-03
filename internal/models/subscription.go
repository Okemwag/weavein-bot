package models

import (
	"errors"
	"time"
)

// PaidThrough accepts Telegram's authoritative subscription expiration.
func PaidThrough(expiration int64, paidAt time.Time) (time.Time, error) {
	if expiration <= paidAt.Unix() {
		return time.Time{}, errors.New("invalid Telegram subscription expiration")
	}
	return time.Unix(expiration, 0).UTC(), nil
}
