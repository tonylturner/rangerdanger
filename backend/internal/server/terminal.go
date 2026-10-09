package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// resizeMsg is a client → server message to resize the PTY.
type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// handleWorkshopTerminal handles WebSocket terminal connections for workshop
// mode: the node's container comes from the range manifest.
func (s *Server) handleWorkshopTerminal(c *gin.Context) {
	svc, err := nodeService(rangeOf(c), c.Param("nodeId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	s.connectTerminal(c, svc)
}

// handleTerminal handles WebSocket connections for a lab instance node. The
// instance must exist; its node resolves in the range manifest.
func (s *Server) handleTerminal(c *gin.Context) {
	var instance models.LabInstance
	if err := s.db.First(&instance, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lab not found"})
		return
	}
	svc, err := nodeService(rangeOf(c), c.Param("nodeId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	s.connectTerminal(c, svc)
}

// connectTerminal upgrades the connection to WebSocket and connects to the
// service's container. The firewall lands in its appliance CLI.
func (s *Server) connectTerminal(c *gin.Context, svc manifest.Service) {
	// Upgrade HTTP connection to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	// Execute shell in container (using container name)
	hijack, execID, err := s.orchestrator.ExecShell(c.Request.Context(), svc.Container, isFirewall(svc))
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()+"\r\n"))
		return
	}
	defer hijack.Close()

	// The session belongs to the range generation: when it stops, closing
	// both ends unblocks the copy loops below.
	stopOnRangeEnd := context.AfterFunc(c.Request.Context(), func() {
		_ = ws.Close()
		hijack.Close()
	})
	defer stopOnRangeEnd()

	// Bidirectional copy between WebSocket and Docker exec
	done := make(chan struct{})

	// Read from container, write to WebSocket
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for {
			n, err := hijack.Reader.Read(buf)
			if err != nil {
				if err != io.EOF {
					ws.WriteMessage(websocket.TextMessage, []byte("\r\n[Connection closed]\r\n"))
				}
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// Read from WebSocket, write to container. Text messages are first
	// checked for resize JSON; everything else is written to the shell stdin.
	go func() {
		for {
			msgType, message, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage && len(message) > 0 && message[0] == '{' {
				var rm resizeMsg
				if err := json.Unmarshal(message, &rm); err == nil && rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
					_ = s.orchestrator.ResizeExec(c.Request.Context(), execID, uint(rm.Cols), uint(rm.Rows))
					continue
				}
			}
			if _, err := hijack.Conn.Write(message); err != nil {
				return
			}
		}
	}()

	// Wait for connection to close
	<-done
}
