package provider

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid duration %q: must be a Go duration or end with 'd' for days", s)
}
