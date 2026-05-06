package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/tturner/rangerdanger/backend/internal/labs"
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

// handleWorkshopTerminal handles WebSocket terminal connections for workshop mode.
// Resolves nodes from the workshop template instead of a lab instance.
func (s *Server) handleWorkshopTerminal(c *gin.Context) {
	nodeID := c.Param("nodeId")

	nodeConfig, err := s.resolveWorkshopNode(nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	s.connectTerminal(c, nodeConfig)
}

// handleTerminal handles WebSocket connections for container terminal access.
func (s *Server) handleTerminal(c *gin.Context) {
	labID := c.Param("id")
	nodeID := c.Param("nodeId")

	// Get lab instance with template to access topology
	var instance models.LabInstance
	if err := s.db.Preload("Template").First(&instance, "id = ?", labID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lab not found"})
		return
	}

	// Parse topology to find node config
	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(instance.Template.Topology), &topo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}

	// Find the node in topology
	var nodeConfig *labs.NodeYAML
	for i := range topo.Nodes {
		if topo.Nodes[i].ID == nodeID {
			nodeConfig = &topo.Nodes[i]
			break
		}
	}
	if nodeConfig == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found in topology"})
		return
	}

	s.connectTerminal(c, nodeConfig)
}

// resolveWorkshopNode finds a node in the workshop template topology.
func (s *Server) resolveWorkshopNode(nodeID string) (*labs.NodeYAML, error) {
	var template models.LabTemplate
	if err := s.db.First(&template, "id = ?", workshopTemplateID).Error; err != nil {
		return nil, fmt.Errorf("workshop template not found")
	}

	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(template.Topology), &topo); err != nil {
		return nil, fmt.Errorf("invalid topology")
	}

	for i := range topo.Nodes {
		if topo.Nodes[i].ID == nodeID {
			return &topo.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("node %s not found in topology", nodeID)
}

// connectTerminal upgrades the connection to WebSocket and connects to the container.
func (s *Server) connectTerminal(c *gin.Context, nodeConfig *labs.NodeYAML) {
	// containd_ngfw previously used SSH, but the containd image's
	// built-in SSH server changed auth between versions and breaks
	// unpredictably. Docker exec to /bin/bash is reliable and gives
	// the same Linux shell (CONTAIND_SSH_SHELL_MODE=linux is set).
	// The SSH handler is kept below as a fallback if needed.

	// Get container name from topology
	containerName := nodeConfig.Container
	if containerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no container configured for node"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	// Execute shell in container (using container name)
	hijack, execID, err := s.orchestrator.ExecShell(c.Request.Context(), containerName)
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

