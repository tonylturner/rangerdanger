package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// Firewall session routes: the range firewall's conntrack table and the
// deletion of one entry. They are generic: every package's firewall is
// containd. Deleting an entry is not a reset; the flow's next packet is new
// and meets the active policy, so it dies only if that policy denies it.

// sessionListLimit is how many conntrack entries the list reads; filtering
// happens here, so the whole table is fetched.
const sessionListLimit = 5000

// sessionList is GET /api/firewall/sessions.
type sessionList struct {
	Sessions []containd.Session `json:"sessions"`
	Count    int                `json:"count"`
}

// handleFirewallSessions lists the firewall's conntrack entries, optionally
// only those with ?dport= and ?src=.
func (s *Server) handleFirewallSessions(c *gin.Context) {
	var dport *int
	if raw, ok := c.GetQuery("dport"); ok {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dport must be a port number 1-65535"})
			return
		}
		dport = &port
	}
	src, filterSrc := c.GetQuery("src")
	if filterSrc && !isIPv4(src) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src must be an IPv4 address"})
		return
	}

	all, err := rangeOf(c).Containd().ListSessions(c.Request.Context(), sessionListLimit)
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	sessions := make([]containd.Session, 0, len(all))
	for _, session := range all {
		if dport != nil && (session.Dport == nil || *session.Dport != *dport) {
			continue
		}
		if filterSrc && session.Src != src {
			continue
		}
		sessions = append(sessions, session)
	}
	c.JSON(http.StatusOK, sessionList{Sessions: sessions, Count: len(sessions)})
}

// handleFirewallSessionKill deletes the conntrack entry with the exact
// original-direction tuple.
func (s *Server) handleFirewallSessionKill(c *gin.Context) {
	var tuple containd.SessionTuple
	if !bindStrictJSON(c, &tuple) {
		return
	}
	if err := validateSessionTuple(tuple); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := rangeOf(c).Containd().KillSession(c.Request.Context(), tuple)
	switch {
	case errors.Is(err, containd.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "no conntrack entry " + tupleText(tuple), "detail": err.Error()})
	case err != nil:
		upstreamFailed(c, err)
	default:
		c.JSON(http.StatusOK, gin.H{"status": "deleted"})
	}
}

// validateSessionTuple requires a full tcp or udp IPv4 tuple. Zero ports
// are rejected: containd deletes port zero literally, never as a wildcard.
func validateSessionTuple(tuple containd.SessionTuple) error {
	if tuple.Proto != "tcp" && tuple.Proto != "udp" {
		return fmt.Errorf("proto must be tcp or udp")
	}
	if !isIPv4(tuple.Src) || !isIPv4(tuple.Dst) {
		return fmt.Errorf("src and dst must be IPv4 addresses")
	}
	if tuple.Sport < 1 || tuple.Sport > 65535 || tuple.Dport < 1 || tuple.Dport > 65535 {
		return fmt.Errorf("sport and dport must be port numbers 1-65535")
	}
	return nil
}

func tupleText(tuple containd.SessionTuple) string {
	return fmt.Sprintf("%s %s:%d to %s:%d", tuple.Proto, tuple.Src, tuple.Sport, tuple.Dst, tuple.Dport)
}

func isIPv4(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.To4() != nil && ip.String() == value
}
