package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
	modelCmd   *exec.Cmd
	modelProc  *os.Process
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

	inf := &Inferencer{
		logger:     logger,
		modelPath:  modelPath,
		httpClient: httpClient,
		apiURL:     "http://localhost:8000/v1/generate", // Локальный API URL
		useAPI:     true,                                // По умолчанию используем API
	}

	// Запускаем API сервер модели, если он будет использоваться
	if inf.useAPI {
		err := inf.startModelServer()
		if err != nil {
			logger.Error("Failed to start model server: %v", err)
			// Если не удалось запустить API, будем использовать прямой вызов Python
			inf.useAPI = false
		}
	}

	return inf
}

// startModelServer запускает локальный API сервер модели
func (i *Inferencer) startModelServer() error {
	i.logger.Info("Starting model API server...")

	// Проверяем, запущен ли уже сервер
	resp, err := http.Get(i.apiURL + "/health")
	if err == nil && resp.StatusCode == http.StatusOK {
		i.logger.Info("Model API server is already running")
		return nil
	}

	// Запускаем API сервер с помощью Python скрипта
	// Создаем временную директорию для скрипта
	scriptDir := "/tmp/smollm_api"
	os.MkdirAll(scriptDir, 0755)

	// Создаем Python скрипт для запуска сервера
	serverScript := `
import torch
from transformers import pipeline, AutoModelForCausalLM, AutoTokenizer
from fastapi import FastAPI, HTTPException, Body
from pydantic import BaseModel
import uvicorn
import time
import os
import sys

# Определяем модель и токенизатор
model_path = os.environ.get("MODEL_PATH", "")
if not model_path:
    print("MODEL_PATH not set")
    sys.exit(1)

print(f"Loading model from {model_path}")
tokenizer = AutoTokenizer.from_pretrained(model_path)
model = AutoModelForCausalLM.from_pretrained(
    model_path, 
    torch_dtype=torch.float16, 
    device_map="auto", 
    load_in_4bit=True
)

generator = pipeline(
    "text-generation",
    model=model,
    tokenizer=tokenizer
)

# Создаем FastAPI приложение
app = FastAPI(title="SmolLM2 API")

class GenerateRequest(BaseModel):
    prompt: str
    max_tokens: int = 512
    temperature: float = 0.7
    top_p: float = 0.9
    top_k: int = 40
    stop_tokens: list = []
    seed: int = None

class GenerateResponse(BaseModel):
    text: str
    tokens_used: int
    generated_in: float
    prompt_tokens: int

@app.get("/health")
def health_check():
    return {"status": "ok"}

@app.post("/v1/generate")
def generate(request: GenerateRequest = Body(...)):
    start_time = time.time()
    
    # Устанавливаем seed если он указан
    if request.seed is not None:
        torch.manual_seed(request.seed)
    
    # Вычисляем количество токенов в промпте
    prompt_tokens = len(tokenizer.encode(request.prompt))
    
    # Генерируем ответ
    outputs = generator(
        request.prompt,
        max_new_tokens=request.max_tokens,
        temperature=request.temperature,
        top_p=request.top_p,
        top_k=request.top_k,
        do_sample=True,
        pad_token_id=tokenizer.eos_token_id
    )
    
    # Получаем сгенерированный текст
    generated_text = outputs[0]["generated_text"]
    
    # Отрезаем промпт, чтобы получить только сгенерированный текст
    if generated_text.startswith(request.prompt):
        generated_text = generated_text[len(request.prompt):]
    
    # Если есть стоп-токены, обрезаем по ним
    for stop_token in request.stop_tokens:
        if stop_token in generated_text:
            generated_text = generated_text.split(stop_token)[0]
    
    # Общее количество использованных токенов
    total_tokens = len(tokenizer.encode(generated_text)) + prompt_tokens
    
    # Время генерации
    generation_time = time.time() - start_time
    
    return GenerateResponse(
        text=generated_text,
        tokens_used=total_tokens,
        generated_in=generation_time,
        prompt_tokens=prompt_tokens
    )

if __name__ == "__main__":
    uvicorn.run(app, host="localhost", port=8000)
`

	scriptPath := scriptDir + "/server.py"
	err = os.WriteFile(scriptPath, []byte(serverScript), 0755)
	if err != nil {
		return fmt.Errorf("ошибка создания скрипта сервера: %v", err)
	}

	// Запускаем сервер
	cmd := exec.Command("python", "-m", "venv", scriptDir+"/venv")
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("ошибка создания виртуального окружения: %v", err)
	}

	// Устанавливаем зависимости
	cmd = exec.Command(scriptDir+"/venv/bin/pip", "install", "torch", "transformers", "fastapi", "uvicorn", "pydantic")
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("ошибка установки зависимостей: %v", err)
	}

	// Запускаем сервер в фоновом режиме
	i.modelCmd = exec.Command(scriptDir+"/venv/bin/python", scriptPath)
	i.modelCmd.Env = append(os.Environ(), "MODEL_PATH="+i.modelPath)

	// Перенаправляем вывод в файл
	logFile, err := os.Create(scriptDir + "/server.log")
	if err != nil {
		return fmt.Errorf("ошибка создания файла логов: %v", err)
	}

	i.modelCmd.Stdout = logFile
	i.modelCmd.Stderr = logFile

	err = i.modelCmd.Start()
	if err != nil {
		return fmt.Errorf("ошибка запуска сервера: %v", err)
	}

	i.modelProc = i.modelCmd.Process
	i.logger.Info("Model API server started, PID: %d", i.modelProc.Pid)

	// Ждем запуска сервера
	for attempts := 0; attempts < 30; attempts++ {
		resp, err := http.Get(i.apiURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			i.logger.Info("Model API server is now running")
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("таймаут ожидания запуска сервера")
}

// Close освобождает ресурсы и завершает процессы
func (i *Inferencer) Close() {
	// Если запущен API сервер, останавливаем его
	if i.modelProc != nil {
		i.logger.Info("Stopping model API server...")
		i.modelProc.Kill()
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

// generateLocally выполняет генерацию локально через Python
func (i *Inferencer) generateLocally(ctx context.Context, prompt string, maxTokens int, temperature float64, topP float64) (string, error) {
	i.logger.Info("Local inference for prompt length: %d", len(prompt))

	// Создаем временную директорию для скрипта
	scriptDir := "/tmp/smollm_inference"
	os.MkdirAll(scriptDir, 0755)

	// Создаем Python скрипт для инференса
	script := fmt.Sprintf(`
import torch
from transformers import pipeline, AutoModelForCausalLM, AutoTokenizer
import json
import sys

# Аргументы инференса
prompt = %q
max_tokens = %d
temperature = %f
top_p = %f

# Загружаем модель и токенизатор
model_path = %q
tokenizer = AutoTokenizer.from_pretrained(model_path)
model = AutoModelForCausalLM.from_pretrained(
    model_path, 
    torch_dtype=torch.float16, 
    device_map="auto", 
    load_in_4bit=True
)

generator = pipeline(
    "text-generation",
    model=model,
    tokenizer=tokenizer
)

# Генерируем текст
outputs = generator(
    prompt,
    max_new_tokens=max_tokens,
    temperature=temperature,
    top_p=top_p,
    do_sample=True,
    pad_token_id=tokenizer.eos_token_id
)

# Получаем сгенерированный текст
generated_text = outputs[0]["generated_text"]

# Отрезаем промпт, чтобы получить только сгенерированный текст
result = generated_text[len(prompt):]

# Выводим результат в JSON
result_obj = {
    "text": result,
    "tokens_used": len(tokenizer.encode(generated_text)),
    "prompt_tokens": len(tokenizer.encode(prompt))
}

json.dump(result_obj, sys.stdout)
`, prompt, maxTokens, temperature, topP, i.modelPath)

	scriptPath := scriptDir + "/inference.py"
	err := os.WriteFile(scriptPath, []byte(script), 0755)
	if err != nil {
		return "", fmt.Errorf("ошибка создания скрипта инференса: %v", err)
	}

	// Создаем виртуальное окружение если его еще нет
	venvPath := scriptDir + "/venv"
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		cmd := exec.Command("python", "-m", "venv", venvPath)
		err = cmd.Run()
		if err != nil {
			return "", fmt.Errorf("ошибка создания виртуального окружения: %v", err)
		}

		// Устанавливаем зависимости
		cmd = exec.Command(venvPath+"/bin/pip", "install", "torch", "transformers")
		err = cmd.Run()
		if err != nil {
			return "", fmt.Errorf("ошибка установки зависимостей: %v", err)
		}
	}

	// Создаем команду для запуска скрипта
	cmd := exec.CommandContext(ctx, venvPath+"/bin/python", scriptPath)

	// Получаем вывод
	output, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return "", fmt.Errorf("ошибка выполнения скрипта: %v, stderr: %s", err, stderr)
	}

	// Парсим JSON-результат
	var result struct {
		Text         string `json:"text"`
		TokensUsed   int    `json:"tokens_used"`
		PromptTokens int    `json:"prompt_tokens"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("ошибка парсинга вывода: %v, output: %s", err, string(output))
	}

	i.logger.Info("Local inference completed, tokens used: %d", result.TokensUsed)

	return result.Text, nil
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
