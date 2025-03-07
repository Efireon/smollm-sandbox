package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"smollm-sandbox/internal/logging"
)

// InferenceRequest представляет запрос к модели
type InferenceRequest struct {
	Prompt      string   `json:"prompt"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float64  `json:"temperature"`
	TopP        float64  `json:"top_p"`
	TopK        int      `json:"top_k,omitempty"`
	StopTokens  []string `json:"stop_tokens,omitempty"`
	Seed        int      `json:"seed,omitempty"`
}

// InferenceResponse представляет ответ от модели
type InferenceResponse struct {
	Text         string  `json:"text"`
	TokensUsed   int     `json:"tokens_used"`
	GeneratedIn  float64 `json:"generated_in"`
	PromptTokens int     `json:"prompt_tokens"`
}

// Inferencer обеспечивает инференс модели
type Inferencer struct {
	logger     *logging.Logger
	modelPath  string
	httpClient *http.Client
	apiURL     string
	useAPI     bool
}

// NewInferencer создает новый экземпляр Inferencer
func NewInferencer(modelPath string) *Inferencer {
	logger := logging.NewLogger()

	// Проверяем существование модели
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		logger.Warn("Model path does not exist: %s", modelPath)
	}

	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}

	return &Inferencer{
		logger:     logger,
		modelPath:  modelPath,
		httpClient: httpClient,
		apiURL:     "http://localhost:8080/v1/generate", // URL локального API, если используется
		useAPI:     false,                               // По умолчанию используем локальную модель
	}
}

// SetUseAPI устанавливает режим использования API
func (i *Inferencer) SetUseAPI(useAPI bool) {
	i.useAPI = useAPI
}

// SetAPIURL устанавливает URL API
func (i *Inferencer) SetAPIURL(apiURL string) {
	i.apiURL = apiURL
}

// Generate выполняет генерацию текста с помощью модели
func (i *Inferencer) Generate(ctx context.Context, prompt string, maxTokens int, temperature float64, topP float64) (string, error) {
	if i.useAPI {
		return i.generateViaAPI(ctx, prompt, maxTokens, temperature, topP)
	} else {
		return i.generateLocally(ctx, prompt, maxTokens, temperature, topP)
	}
}

// generateViaAPI выполняет генерацию через HTTP API
func (i *Inferencer) generateViaAPI(ctx context.Context, prompt string, maxTokens int, temperature float64, topP float64) (string, error) {
	// Создаем запрос
	request := InferenceRequest{
		Prompt:      prompt,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		TopP:        topP,
		StopTokens:  []string{"\n\n"},
	}

	// Сериализуем в JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	// Создаем HTTP запрос
	req, err := http.NewRequestWithContext(ctx, "POST", i.apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("ошибка создания HTTP запроса: %v", err)
	}

	// Устанавливаем заголовки
	req.Header.Set("Content-Type", "application/json")

	// Выполняем запрос
	start := time.Now()
	resp, err := i.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения HTTP запроса: %v", err)
	}
	defer resp.Body.Close()

	// Проверяем статус
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ошибка API: %s, код: %d", string(body), resp.StatusCode)
	}

	// Читаем ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	// Разбираем JSON
	var response InferenceResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("ошибка разбора JSON: %v", err)
	}

	elapsed := time.Since(start)
	i.logger.Info("API inference completed in %v, tokens used: %d", elapsed, response.TokensUsed)

	return response.Text, nil
}

// generateLocally выполняет генерацию локально
func (i *Inferencer) generateLocally(ctx context.Context, prompt string, maxTokens int, temperature float64, topP float64) (string, error) {
	// TODO: Здесь должен быть код для локального инференса модели
	// Это потребует интеграции с библиотекой машинного обучения

	i.logger.Info("Local inference for prompt length: %d", len(prompt))

	// Пока используем заглушку
	time.Sleep(500 * time.Millisecond)

	// Эмулируем генерацию ответа
	responses := []string{
		"Я считаю, что для решения этой задачи подойдет алгоритм сортировки слиянием, так как он имеет хорошую производительность O(n log n) на больших наборах данных.",
		"Давайте рассмотрим проблему с точки зрения теории графов. Если представить задачу как граф, то мы можем использовать алгоритм поиска в ширину (BFS).",
		"Исходя из анализа данных, могу сказать, что наиболее эффективное решение - использовать хеш-таблицу для быстрого поиска.",
		"Для оптимизации производительности вычислений можно распараллелить обработку данных, разделив задачу на несколько потоков.",
		"Я бы рекомендовал использовать динамическое программирование для решения этой задачи, поскольку она имеет оптимальную подструктуру.",
	}

	// Выбираем ответ на основе длины промпта
	index := len(prompt) % len(responses)
	return responses[index], nil
}

// ThinkingGenerate генерирует текст в режиме размышления
func (i *Inferencer) ThinkingGenerate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	// Для режима размышления используем фиксированные параметры
	temperature := 0.9 // Более высокая температура для креативности
	topP := 0.95

	// Добавляем контекст размышления
	thinkingPrompt := "Размышляю самостоятельно без участия пользователя: " + prompt

	// Генерируем текст
	result, err := i.Generate(ctx, thinkingPrompt, maxTokens, temperature, topP)
	if err != nil {
		return "", err
	}

	return result, nil
}
