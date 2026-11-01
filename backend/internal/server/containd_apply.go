package server

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// writeApplyError keeps upstream conflicts distinct from backend/API failures.
func writeApplyError(c *gin.Context, err error, applied *containd.ApplyResult, fields gin.H) {
	status := http.StatusBadGateway
	fields["error"] = err.Error()
	if errors.Is(err, errRangeStopped) {
		status = http.StatusServiceUnavailable
	}
	var upstream *containd.ConfigWriteError
	if errors.As(err, &upstream) && upstream.Status == http.StatusPreconditionFailed {
		status = http.StatusConflict
		fields["error"] = containd.ConfigConflictMessage
		fields["code"] = containd.ConfigConflictCode
	}
	if applied != nil && applied.Receipt != nil {
		fields["containd"] = applied.Receipt
	}
	var protection *containd.ProtectionError
	if errors.As(err, &protection) {
		fields["observation"] = protection.Observation
	}
	c.JSON(status, fields)
}
