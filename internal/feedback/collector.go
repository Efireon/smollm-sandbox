package feedback

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"smollm-sandbox/internal/logging"
)

// FeedbackType определяет тип обратной связи
type FeedbackType string

const (
	ModelOutput   FeedbackType = "model_output"   // Обратная связь по выводу модели
	CodeExecution FeedbackType = "code_execution" // Обратная связь по выполнению кода
	SystemError   FeedbackType = "system_error"   // Обратная связь по системным ошибкам
	Thinking      FeedbackType = "thinking"       // Обратная связь по режиму размышления
)

// FeedbackItem представляет элемент обратной связи
type FeedbackItem struct {
	ID        string       `json:"id"`
	Type      FeedbackType `json:"type"`
	Content   string       `json:"content"`
	Rating    int          `json:"rating"` // От 1 до 5
	Comment   string       `json:"comment"`
	Timestamp time.Time    `json:"timestamp"`
	Metadata  interface{}  `json:"metadata,omitempty"`
}

// Collector обеспечивает сбор и сохранение обратной связи
type Collector struct {
	logger      *logging.Logger
	feedbackDir string
	items       []FeedbackItem
	mu          sync.Mutex
}

// NewCollector создает новый экземпляр collector
func NewCollector(feedbackDir string) *Collector {
	logger := logging.NewLogger()

	// Создаем директорию для обратной связи, если ее нет
	if _, err := os.Stat(feedbackDir); os.IsNotExist(err) {
		os.MkdirAll(feedbackDir, 0755)
	}

	return &Collector{
		logger:      logger,
		feedbackDir: feedbackDir,
		items:       make([]FeedbackItem, 0),
	}
}

// AddFeedback добавляет новый элемент обратной связи
func (c *Collector) AddFeedback(feedbackType FeedbackType, content string, rating int, comment string, metadata interface{}) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Проверяем рейтинг
	if rating < 1 || rating > 5 {
		return "", fmt.Errorf("рейтинг должен быть от 1 до 5")
	}

	// Создаем ID для обратной связи
	id := fmt.Sprintf("feedback_%d", time.Now().Unix())

	// Создаем элемент обратной связи
	item := FeedbackItem{
		ID:        id,
		Type:      feedbackType,
		Content:   content,
		Rating:    rating,
		Comment:   comment,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	// Добавляем в список
	c.items = append(c.items, item)

	// Сохраняем в файл
	if err := c.saveFeedback(item); err != nil {
		c.logger.Error("Failed to save feedback: %v", err)
		return id, err
	}

	c.logger.Info("Added feedback: %s, type: %s, rating: %d", id, feedbackType, rating)
	return id, nil
}

// saveFeedback сохраняет элемент обратной связи в файл
func (c *Collector) saveFeedback(item FeedbackItem) error {
	// Создаем имя файла
	fileName := fmt.Sprintf("%s_%s.json", item.ID, item.Type)
	filePath := filepath.Join(c.feedbackDir, fileName)

	// Сериализуем в JSON
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("ошибка сериализации в JSON: %v", err)
	}

	// Записываем в файл
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("ошибка записи в файл: %v", err)
	}

	return nil
}

// GetFeedback возвращает элемент обратной связи по ID
func (c *Collector) GetFeedback(id string) (FeedbackItem, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, item := range c.items {
		if item.ID == id {
			return item, true
		}
	}

	return FeedbackItem{}, false
}

// GetAllFeedback возвращает все элементы обратной связи
func (c *Collector) GetAllFeedback() []FeedbackItem {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Создаем копию, чтобы избежать гонок данных
	result := make([]FeedbackItem, len(c.items))
	copy(result, c.items)

	return result
}

// GetFeedbackByType возвращает элементы обратной связи указанного типа
func (c *Collector) GetFeedbackByType(feedbackType FeedbackType) []FeedbackItem {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []FeedbackItem
	for _, item := range c.items {
		if item.Type == feedbackType {
			result = append(result, item)
		}
	}

	return result
}

// LoadFeedbackFromDisk загружает элементы обратной связи из файлов
func (c *Collector) LoadFeedbackFromDisk() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Очищаем текущий список
	c.items = make([]FeedbackItem, 0)

	// Читаем файлы из директории
	files, err := os.ReadDir(c.feedbackDir)
	if err != nil {
		return fmt.Errorf("ошибка чтения директории: %v", err)
	}

	// Обрабатываем каждый файл
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			filePath := filepath.Join(c.feedbackDir, file.Name())

			// Читаем содержимое файла
			data, err := os.ReadFile(filePath)
			if err != nil {
				c.logger.Warn("Failed to read feedback file %s: %v", file.Name(), err)
				continue
			}

			// Разбираем JSON
			var item FeedbackItem
			if err := json.Unmarshal(data, &item); err != nil {
				c.logger.Warn("Failed to parse feedback file %s: %v", file.Name(), err)
				continue
			}

			// Добавляем в список
			c.items = append(c.items, item)
		}
	}

	c.logger.Info("Loaded %d feedback items from disk", len(c.items))
	return nil
}

// GetFeedbackStats возвращает статистику по обратной связи
func (c *Collector) GetFeedbackStats() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := map[string]interface{}{
		"total_count": len(c.items),
		"type_counts": make(map[FeedbackType]int),
		"rating_avg":  0.0,
		"ratings":     make(map[int]int),
	}

	// Считаем статистику
	var totalRating int
	for _, item := range c.items {
		// Счетчики по типам
		stats["type_counts"].(map[FeedbackType]int)[item.Type]++

		// Счетчики по рейтингам
		stats["ratings"].(map[int]int)[item.Rating]++

		// Для средней оценки
		totalRating += item.Rating
	}

	// Рассчитываем среднюю оценку
	if len(c.items) > 0 {
		stats["rating_avg"] = float64(totalRating) / float64(len(c.items))
	}

	return stats
}
