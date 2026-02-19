package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
}

func NewClient(host string, port int, timeout time.Duration) *Client {
	return &Client{
		Host:    host,
		Port:    port,
		Timeout: timeout,
	}
}

func (c *Client) Chat(ctx context.Context, model string, messages []Message, onResponse func(string) error) ([]Message, error) {
	url := fmt.Sprintf("http://%s:%d/api/chat", c.Host, c.Port)

	reqBody, err := json.Marshal(ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call ollama api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse string
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("error reading stream: %w", err)
		}

		var chatResp ChatResponse
		if err := json.Unmarshal(line, &chatResp); err != nil {
			continue
		}

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
