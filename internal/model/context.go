package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Context представляет контекст сессии модели
type Context struct {
	SessionID string       `json:"session_id"`
	Messages  []Message    `json:"messages"`
	Metadata  ContextMeta  `json:"metadata"`
	State     ContextState `json:"state"`
}

// Message представляет одно сообщение в контексте
type Message struct {
	Role      string    `json:"role"`      // "user", "assistant" или "system"
	Content   string    `json:"content"`   // Содержимое сообщения
	Timestamp time.Time `json:"timestamp"` // Время создания сообщения
}

// ContextMeta содержит метаданные контекста
type ContextMeta struct {
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Properties map[string]string `json:"properties"`
}

// ContextState содержит состояние контекста
type ContextState struct {
	Mode            string  `json:"mode"`             // "chat", "thinking" и т.д.
	TokensUsed      int     `json:"tokens_used"`      // Количество использованных токенов
	Temperature     float64 `json:"temperature"`      // Параметр temperature
	TopP            float64 `json:"top_p"`            // Параметр top_p
	ThinkingEnabled bool    `json:"thinking_enabled"` // Включен ли режим размышления
}

// NewContext создает новый контекст
func NewContext() *Context {
	now := time.Now()
	return &Context{
		SessionID: fmt.Sprintf("session_%d", now.Unix()),
		Messages:  []Message{},
		Metadata: ContextMeta{
			CreatedAt:  now,
			UpdatedAt:  now,
			Properties: make(map[string]string),
		},
		State: ContextState{
			Mode:            "chat",
			TokensUsed:      0,
			Temperature:     0.7,
			TopP:            0.9,
			ThinkingEnabled: false,
		},
	}
}

// AddUserMessage добавляет сообщение пользователя в контекст
func (c *Context) AddUserMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	c.Metadata.UpdatedAt = time.Now()
}

// AddAssistantMessage добавляет сообщение ассистента в контекст
func (c *Context) AddAssistantMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      "assistant",
		Content:   content,
		Timestamp: time.Now(),
	})
	c.Metadata.UpdatedAt = time.Now()
}

// AddSystemMessage добавляет системное сообщение в контекст
func (c *Context) AddSystemMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	})
	c.Metadata.UpdatedAt = time.Now()
}

// SetProperty устанавливает свойство в метаданных
func (c *Context) SetProperty(key, value string) {
	c.Metadata.Properties[key] = value
	c.Metadata.UpdatedAt = time.Now()
}

// GetProperty возвращает свойство из метаданных
func (c *Context) GetProperty(key string) (string, bool) {
	value, exists := c.Metadata.Properties[key]
	return value, exists
}

// ToJSON сериализует контекст в JSON
func (c *Context) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}

// FromJSON десериализует контекст из JSON
func (c *Context) FromJSON(data []byte) error {
	return json.Unmarshal(data, c)
}

// GetLastUserMessage возвращает последнее сообщение пользователя
func (c *Context) GetLastUserMessage() (Message, bool) {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "user" {
			return c.Messages[i], true
		}
	}
	return Message{}, false
}

// GetLastAssistantMessage возвращает последнее сообщение ассистента
func (c *Context) GetLastAssistantMessage() (Message, bool) {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" {
			return c.Messages[i], true
		}
	}
	return Message{}, false
}

// SetMode устанавливает режим контекста
func (c *Context) SetMode(mode string) {
	c.State.Mode = mode
	c.Metadata.UpdatedAt = time.Now()
}

// SetTemperature устанавливает параметр temperature
func (c *Context) SetTemperature(temperature float64) {
	c.State.Temperature = temperature
	c.Metadata.UpdatedAt = time.Now()
}

// SetTopP устанавливает параметр top_p
func (c *Context) SetTopP(topP float64) {
	c.State.TopP = topP
	c.Metadata.UpdatedAt = time.Now()
}

// EnableThinking включает режим размышления
func (c *Context) EnableThinking(enabled bool) {
	c.State.ThinkingEnabled = enabled
	c.Metadata.UpdatedAt = time.Now()
}

// GetSummary возвращает краткую информацию о контексте
func (c *Context) GetSummary() map[string]any {
	return map[string]any{
		"session_id":       c.SessionID,
		"message_count":    len(c.Messages),
		"created_at":       c.Metadata.CreatedAt,
		"updated_at":       c.Metadata.UpdatedAt,
		"mode":             c.State.Mode,
		"tokens_used":      c.State.TokensUsed,
		"thinking_enabled": c.State.ThinkingEnabled,
	}
}
