package queue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client sends messages to an Azure Storage Queue via a SAS URL.
// Set AZURE_QUEUE_SAS_URL to the full SAS URL for the /messages endpoint, e.g.:
// https://<account>.queue.windows.net/<queue>/messages?sv=...&sig=...
type Client struct {
	sasURL     string
	httpClient *http.Client
}

func New(sasURL string) *Client {
	return &Client{
		sasURL:     sasURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Send encodes payload as JSON, base64-encodes it, and posts it to the queue.
func (c *Client) Send(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	body := "<QueueMessage><MessageText>" + encoded + "</MessageText></QueueMessage>"

	req, err := http.NewRequest(http.MethodPost, c.sasURL, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send to queue: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("queue returned unexpected status: %d", resp.StatusCode)
	}
	return nil
}
