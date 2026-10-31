package containd

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// GetConfigDocument reads the running configuration for tuple evaluation and
// canonical identity checks. It never reads candidate or protocol state.
func (c *Client) GetConfigDocument(ctx context.Context) ([]byte, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, c.BaseURL+"/api/v1/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get config returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
