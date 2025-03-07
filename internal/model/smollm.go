package model

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"smollm-sandbox/internal/logging"
)

const (
	// Константы для работы с моделью
	MODEL_PATH    = "/opt/smollm-models/SmolLM2-135M-Instruct"
	MAX_TOKENS    = 2048
	TEMPERATURE   = 0.7
	TOP_P         = 0.9
	THINKING_SEED = 42 // Seed для режима размышления
)

// SmolLM представляет интерфейс для работы с моделью SmolLM2
type SmolLM struct {
	logger      *logging.Logger
	modelPath   string
	contextSize int
	temperature float64
	topP        float64
	history     []ContextEntry
	mutex       sync.Mutex
	inferencer  *Inferencer // Интерфейс для инференса модели
	context     *Context    // Управление контекстом
}

// ContextEntry представляет одну запись в истории контекста
type ContextEntry struct {
	Role    string
	Content string
	Time    time.Time
}

// NewSmolLM создает новый экземпляр SmolLM
func NewSmolLM() *SmolLM {
	logger := logging.NewLogger()
	logger.Info("Initializing SmolLM2 model")

	// Создаем контекст
	ctx := NewContext()
	ctx.AddSystemMessage("Ты SmolLM2, маленькая, но умная языковая модель. Ты можешь писать код, объяснять понятия и размышлять на разные темы.")

	// Создаем объект для инференса
	inferencer := NewInferencer(MODEL_PATH)

	return &SmolLM{
		logger:      logger,
		modelPath:   MODEL_PATH,
		contextSize: MAX_TOKENS,
		temperature: TEMPERATURE,
		topP:        TOP_P,
		history:     make([]ContextEntry, 0),
		inferencer:  inferencer,
		context:     ctx,
	}
}

// Process обрабатывает ввод пользователя и возвращает ответ модели
func (s *SmolLM) Process(input string) string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Добавляем ввод пользователя в историю и контекст
	s.history = append(s.history, ContextEntry{
		Role:    "user",
		Content: input,
		Time:    time.Now(),
	})
	s.context.AddUserMessage(input)

	// Подготовка контекста для модели
	contextStr := s.prepareContext()

	// Вызываем модель с контекстом
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	response, err := s.inferencer.Generate(ctx, contextStr, MAX_TOKENS/2, s.temperature, s.topP)
	if err != nil {
		s.logger.Error("Inference error: %v", err)
		response = "Извините, произошла ошибка при обработке запроса. Пожалуйста, попробуйте еще раз."
	}

	// Добавляем ответ в историю и контекст
	s.history = append(s.history, ContextEntry{
		Role:    "assistant",
		Content: response,
		Time:    time.Now(),
	})
	s.context.AddAssistantMessage(response)

	// Обрезаем историю, если она слишком длинная
	s.truncateHistory()

	return response
}

// Think запускает режим размышления без входных данных пользователя
func (s *SmolLM) Think(seconds int, outputFile string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.logger.Info("Starting thinking mode for %d seconds", seconds)

	// Создаем файл для записи размышлений
	file, err := os.Create(outputFile)
	if err != nil {
		s.logger.Error("Failed to create output file: %v", err)
		return
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// Запись заголовка
	timestamp := time.Now().Format(time.RFC3339)
	writer.WriteString(fmt.Sprintf("# SmolLM2 Thoughts - %s\n\n", timestamp))

	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()

	// Начальный затравочный текст для размышлений
	prompt := "Размышляю самостоятельно без участия пользователя. Могу думать о программировании, философии, науке и других интересных темах. Начну со свободного потока мыслей."

	writer.WriteString("## Начало размышлений\n\n")

	// Запрашиваем первую мысль
	thought, err := s.inferencer.ThinkingGenerate(ctx, prompt, 200)
	if err != nil {
		s.logger.Error("Error in thinking mode: %v", err)
		writer.WriteString("Ошибка в режиме размышления: " + err.Error())
		return
	}

	writer.WriteString(thought + "\n\n")

	// Цикл размышлений
	for {
		select {
		case <-ctx.Done():
			// Время вышло
			writer.WriteString("\n## Конец размышлений\n")
			s.logger.Info("Thinking mode completed, output saved to %s", outputFile)
			return
		default:
			// Продолжаем размышления, используя предыдущую мысль как контекст
			nextThought, err := s.inferencer.ThinkingGenerate(ctx, "Продолжаю размышлять о "+thought, 200)
			if err != nil {
				s.logger.Error("Error in thinking mode: %v", err)
				writer.WriteString("\nОшибка в режиме размышления: " + err.Error() + "\n")
				continue
			}

			writer.WriteString(nextThought + "\n\n")
			thought = nextThought

			// Небольшая пауза между размышлениями
			time.Sleep(2 * time.Second)
		}
	}
}

// SaveSession сохраняет текущую сессию (историю контекста)
func (s *SmolLM) SaveSession(sessionName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Сохраняем контекст
	return s.context.Save(sessionName)
}

// LoadSession загружает сессию
func (s *SmolLM) LoadSession(sessionName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Загружаем контекст
	newContext, err := LoadContext(sessionName)
	if err != nil {
		return err
	}

	s.context = newContext

	// Обновляем внутреннюю историю
	s.history = []ContextEntry{}
	for _, msg := range s.context.Messages {
		s.history = append(s.history, ContextEntry{
			Role:    msg.Role,
			Content: msg.Content,
			Time:    msg.Timestamp,
		})
	}

	s.logger.Info("Session loaded: %s", sessionName)
	return nil
}

// Close освобождает ресурсы
func (s *SmolLM) Close() {
	s.inferencer.Close()
}

// Вспомогательные методы

// prepareContext готовит контекст для модели на основе истории
func (s *SmolLM) prepareContext() string {
	var contextStr string

	// Добавляем системное сообщение
	systemMsg, found := s.getSystemMessage()
	if found {
		contextStr += "Системная инструкция: " + systemMsg + "\n\n"
	}

	// Добавляем историю диалога
	for _, entry := range s.history {
		prefix := ""
		if entry.Role == "user" {
			prefix = "Человек: "
		} else if entry.Role == "assistant" {
			prefix = "Ассистент: "
		} else {
			continue // Пропускаем системные сообщения
		}
		contextStr += prefix + entry.Content + "\n\n"
	}

	// Добавляем префикс для ответа
	contextStr += "Ассистент: "

	return contextStr
}

// getSystemMessage возвращает системное сообщение из контекста
func (s *SmolLM) getSystemMessage() (string, bool) {
	for _, msg := range s.context.Messages {
		if msg.Role == "system" {
			return msg.Content, true
		}
	}
	return "", false
}

// truncateHistory обрезает историю, чтобы она не превышала максимальный размер
func (s *SmolLM) truncateHistory() {
	// Максимальное количество сообщений в истории
	maxMessages := 10

	// Оставляем только последние maxMessages сообщений
	if len(s.history) > maxMessages {
		s.history = s.history[len(s.history)-maxMessages:]
	}
}
