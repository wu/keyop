package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatResponse struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Message   Message   `json:"message"`
	Done      bool      `json:"done"`
}

type Client struct {
	Host    string
	Port    int
	Timeout time.Duration
	Stream  bool
}

func NewClient(host string, port int, timeout time.Duration, stream bool) *Client {
	return &Client{
		Host:    host,
		Port:    port,
		Timeout: timeout,
		Stream:  stream,
	}
}

func (c *Client) Chat(ctx context.Context, model string, messages []Message, onResponse func(string) error) ([]Message, error) {
	url := fmt.Sprintf("http://%s:%d/api/chat", c.Host, c.Port)

	reqBody, err := json.Marshal(ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   c.Stream,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: c.Timeout}
	resp, err := client.Do(httpReq) //nolint:gosec // request to configured host/port
	if err != nil {
		return nil, fmt.Errorf("failed to call ollama api: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("ollama: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	if !c.Stream {
		var chatResp ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			return nil, fmt.Errorf("failed to decode non-streaming response: %w", err)
		}

		fullResponse := chatResp.Message.Content
		if onResponse != nil {
			if err := onResponse(fullResponse); err != nil {
				return nil, err
			}
		}
		updatedMessages := append(messages, Message{Role: "assistant", Content: fullResponse})
		return updatedMessages, nil
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse string
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("error reading stream: %w", err)
		}

		if len(line) > 0 {
			var chatResp ChatResponse
			if err := json.Unmarshal(line, &chatResp); err == nil {
				content := chatResp.Message.Content
				fullResponse += content
				if onResponse != nil {
					if err := onResponse(content); err != nil {
						return nil, err
					}
				}

				if chatResp.Done {
					break
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	updatedMessages := append(messages, Message{Role: "assistant", Content: fullResponse})
	return updatedMessages, nil
}

func (c *Client) Summarize(ctx context.Context, model string, messages []Message) (Message, error) {
	prompt := "Please summarize the following conversation history concisely:"
	for _, m := range messages {
		prompt += fmt.Sprintf("\n%s: %s", m.Role, m.Content)
	}

	summarizeMessages := []Message{
		{Role: "user", Content: prompt},
	}

	var summary string
	_, err := c.Chat(ctx, model, summarizeMessages, func(content string) error {
		summary += content
		return nil
	})
	if err != nil {
		return Message{}, err
	}

	return Message{Role: "system", Content: "Summary of previous conversation: " + summary}, nil
}
