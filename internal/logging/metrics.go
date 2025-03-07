package logging

import (
	"encoding/json"
	"sync"
	"time"
)

// Metrics хранит метрики работы системы
type Metrics struct {
	StartTime   time.Time      `json:"start_time"`
	LogCounts   map[int]int    `json:"log_counts"`
	ErrorCount  int            `json:"error_count"`
	Executions  int            `json:"executions"`
	ThoughtTime time.Duration  `json:"thought_time"`
	Custom      map[string]any `json:"custom"`
	mu          sync.Mutex
}

// NewMetrics создает новый экземпляр метрик
func NewMetrics() *Metrics {
	return &Metrics{
		StartTime: time.Now(),
		LogCounts: map[int]int{
			DEBUG: 0,
			INFO:  0,
			WARN:  0,
			ERROR: 0,
			FATAL: 0,
		},
		ErrorCount:  0,
		Executions:  0,
		ThoughtTime: 0,
		Custom:      make(map[string]any),
	}
}

// IncrementLogCount увеличивает счетчик логов для указанного уровня
func (m *Metrics) IncrementLogCount(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LogCounts[level]++
}

// IncrementErrorCount увеличивает счетчик ошибок
func (m *Metrics) IncrementErrorCount() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ErrorCount++
}

// IncrementExecutions увеличивает счетчик выполнений кода
func (m *Metrics) IncrementExecutions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Executions++
}

// AddThoughtTime добавляет время размышлений
func (m *Metrics) AddThoughtTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ThoughtTime += duration
}

// SetCustomMetric устанавливает пользовательскую метрику
func (m *Metrics) SetCustomMetric(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Custom[key] = value
}

// GetCustomMetric возвращает пользовательскую метрику
func (m *Metrics) GetCustomMetric(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	value, ok := m.Custom[key]
	return value, ok
}

// GetUptime возвращает время работы системы
func (m *Metrics) GetUptime() time.Duration {
	return time.Since(m.StartTime)
}

// ToJSON сериализует метрики в JSON
func (m *Metrics) ToJSON() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Создаем копию метрик для сериализации
	metrics := map[string]any{
		"start_time":   m.StartTime,
		"log_counts":   m.LogCounts,
		"error_count":  m.ErrorCount,
		"executions":   m.Executions,
		"thought_time": m.ThoughtTime.String(),
		"uptime":       time.Since(m.StartTime).String(),
		"custom":       m.Custom,
	}

	return json.Marshal(metrics)
}
