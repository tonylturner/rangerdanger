package server

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// handleTerminal handles WebSocket connections for container terminal access.
func (s *Server) handleTerminal(c *gin.Context) {
	labID := c.Param("id")
	nodeID := c.Param("nodeId")

	// Get node definition from database
	var node models.NodeDefinition
	if err := s.db.First(&node, "id = ? AND lab_instance_id = ?", nodeID, labID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	// For containd_ngfw, use the firewall container
	containerID := node.ContainerID
	if node.Type == "containd_ngfw" {
		containerID = "rangerdanger-firewall"
	}

	if containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no container associated with node"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	// Execute shell in container
	hijack, err := s.orchestrator.ExecShell(c.Request.Context(), containerID)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()+"\r\n"))
		return
	}
	defer hijack.Close()

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

	// Read from WebSocket, write to container
	go func() {
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if _, err := hijack.Conn.Write(message); err != nil {
				return
			}
		}
	}()

	// Wait for connection to close
	<-done
}
