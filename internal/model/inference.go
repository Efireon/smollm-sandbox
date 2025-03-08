package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	// Создаем временную директорию для скрипта
	scriptDir := "/tmp/smollm_api"
	os.MkdirAll(scriptDir, 0755)

	// Путь к скрипту
	scriptPath := filepath.Join(scriptDir, "server.py")

	// Создаем Python скрипт для запуска сервера
	script := `
import os
import sys
import json
import time
import traceback

# Проверка и установка зависимостей
def install_dependencies():
    import subprocess
    import pkg_resources

    def package_installed(package):
        try:
            pkg_resources.get_distribution(package)
            return True
        except pkg_resources.DistributionNotFound:
            return False

    def install_package(package):
        subprocess.check_call([sys.executable, "-m", "pip", "install", package])

    # Список необходимых пакетов
    required_packages = [
        "torch",
        "transformers", 
        "fastapi", 
        "uvicorn", 
        "pydantic"
    ]

    for package in required_packages:
        if not package_installed(package):
            print(f"Устанавливаю {package}...")
            install_package(package)

# Установка зависимостей перед импортом
install_dependencies()

import torch
from transformers import pipeline, AutoModelForCausalLM, AutoTokenizer
from fastapi import FastAPI, HTTPException, Body
from pydantic import BaseModel
import uvicorn

# Настройки безопасности и совместимости
torch.set_default_dtype(torch.float32)

# Определяем модель и токенизатор
MODEL_PATH = os.environ.get("MODEL_PATH", "")
if not MODEL_PATH:
    print("MODEL_PATH не установлен")
    sys.exit(1)

print(f"Загрузка модели из {MODEL_PATH}")

# Безопасная загрузка модели с обработкой ошибок
try:
    tokenizer = AutoTokenizer.from_pretrained(MODEL_PATH)
    model = AutoModelForCausalLM.from_pretrained(
        MODEL_PATH, 
        torch_dtype=torch.float32,
        device_map="auto"
    )

    generator = pipeline(
        "text-generation",
        model=model,
        tokenizer=tokenizer
    )
except Exception as e:
    print(f"Ошибка при загрузке модели: {e}")
    traceback.print_exc()
    sys.exit(1)

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
    
    # Устанавливаем seed если указан
    if request.seed is not None:
        torch.manual_seed(request.seed)
    
    # Вычисляем количество токенов в промпте
    prompt_tokens = len(tokenizer.encode(request.prompt))
    
    # Генерируем ответ
    try:
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
    except Exception as e:
        print(f"Ошибка генерации: {e}")
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    uvicorn.run(app, host="localhost", port=8000)
`

	// Записываем скрипт в файл
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("ошибка создания скрипта сервера: %v", err)
	}

	// Создаем виртуальное окружение
	venvPath := filepath.Join(scriptDir, "venv")
	cmd := exec.Command("python3", "-m", "venv", venvPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка создания виртуального окружения: %v", err)
	}

	// Запускаем сервер в фоновом режиме
	i.modelCmd = exec.Command(
		filepath.Join(venvPath, "bin", "python"),
		scriptPath,
	)

	// Устанавливаем окружение с путем к модели
	i.modelCmd.Env = append(os.Environ(),
		fmt.Sprintf("MODEL_PATH=%s", i.modelPath),
		fmt.Sprintf("PYTHONPATH=%s", venvPath+"/lib/python3.11/site-packages"),
	)

	// Перенаправляем вывод в файл
	logFile, err := os.Create(filepath.Join(scriptDir, "server.log"))
	if err != nil {
		return fmt.Errorf("ошибка создания файла логов: %v", err)
	}
	i.modelCmd.Stdout = logFile
	i.modelCmd.Stderr = logFile

	// Запускаем процесс
	if err := i.modelCmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска сервера: %v", err)
	}

	i.modelProc = i.modelCmd.Process
	i.logger.Info("Model API server started, PID: %d", i.modelProc.Pid)

	// Ждем запуска сервера с расширенным таймаутом
	for attempts := 0; attempts < 60; attempts++ {
		resp, err := http.Get(i.apiURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			i.logger.Info("Model API server is now running")
			return nil
		}
		time.Sleep(2 * time.Second)
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

	// Путь к скрипту
	scriptPath := filepath.Join(scriptDir, "inference.py")

	// Экранируем специальные символы в промпте
	safePrompt := strings.ReplaceAll(prompt, "\\", "\\\\")
	safePrompt = strings.ReplaceAll(safePrompt, "\"", "\\\"")
	safePrompt = strings.ReplaceAll(safePrompt, "\n", "\\n")

	// Создаем Python скрипт для инференса
	script := fmt.Sprintf(`
import os
import sys
import json
import time
import traceback
import subprocess
import ensurepip

def log(message):
    timestamp = time.strftime("%%Y-%%m-%%d %%H:%%M:%%S")
    print(f"[{timestamp}] {message}", flush=True)

def install_dependencies():
    log("Настройка окружения...")
    
    try:
        # Принудительная установка pip
        log("Обеспечение наличия pip...")
        ensurepip.bootstrap()
        
        log("Установка зависимостей...")
        subprocess.run([
            sys.executable, "-m", "pip", 
            "install", "-U", 
            "--user", 
            "pip", "torch", "transformers"
        ], check=True, capture_output=True)

    except Exception as e:
        log(f"Ошибка установки: {e}")
        # Расширенная диагностика
        try:
            import site
            log(f"User site-packages: {site.getusersitepackages()}")
        except:
            log("Не удалось получить информацию о site-packages")
        raise

log("1. Начало подготовки окружения")

try:
    install_dependencies()

    log("2. Импорт библиотек...")
    import torch
    from transformers import pipeline, AutoModelForCausalLM, AutoTokenizer

    log("3. Загрузка модели...")
    # Загрузка модели
    model_path = %q
    log(f"   Путь к модели: {model_path}")
    
    log("   Загрузка токенизатора...")
    tokenizer = AutoTokenizer.from_pretrained(model_path)
    
    log("   Загрузка модели...")
    model = AutoModelForCausalLM.from_pretrained(
        model_path, 
        torch_dtype=torch.float32
    )

    log("4. Создание генератора...")
    # Создание генератора
    generator = pipeline(
        "text-generation",
        model=model,
        tokenizer=tokenizer
    )

    log("5. Генерация текста...")
    # Генерация текста
    prompt = %q
    max_tokens = %d
    temperature = %f

    log(f"   Промпт: {prompt}")
    log(f"   Макс. токенов: {max_tokens}")
    log(f"   Температура: {temperature}")

    outputs = generator(
        prompt,
        max_new_tokens=max_tokens,
        temperature=temperature,
        do_sample=True
    )

    # Получаем сгенерированный текст
    generated_text = outputs[0]["generated_text"]

    # Отрезаем промпт
    if generated_text.startswith(prompt):
        generated_text = generated_text[len(prompt):]

    log("6. Текст сгенерирован успешно")

    # Возвращаем результат
    result = {
        "text": generated_text,
        "success": True
    }
    
    print(json.dumps(result))

except Exception as e:
    log(f"ОШИБКА: {e}")
    error_result = {
        "text": "",
        "success": False,
        "error": str(e),
        "traceback": traceback.format_exc()
    }
    print(json.dumps(error_result))

log("7. Скрипт завершен")
`, i.modelPath, safePrompt, maxTokens, temperature)

	// Записываем скрипт в файл
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return "", fmt.Errorf("ошибка создания скрипта: %v", err)
	}

	// Создаем команду для выполнения скрипта
	cmd := exec.CommandContext(ctx, "python3", scriptPath)

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
		Text    string `json:"text"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("ошибка парсинга вывода: %v, output: %s", err, string(output))
	}

	if !result.Success {
		return "", fmt.Errorf("ошибка генерации: %s", result.Error)
	}

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
