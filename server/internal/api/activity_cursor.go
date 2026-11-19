package api

import (
	"strconv"
	"strings"
	"time"
)

func (c *Ctx) activityCursor() (string, error) {
	raw := c.R.URL.Query().Get("activityCursor")
	if raw == "" {
		return "", nil
	}
	parts := strings.SplitN(raw, "|", 3)
	if len(parts) != 3 || len(parts[2]) > 64 || parts[2] == "" {
		return "", badRequest("Invalid activity cursor")
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		return "", badRequest("Invalid activity cursor")
	}
	if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
		return "", badRequest("Invalid activity cursor")
	}
	return raw, nil
}
