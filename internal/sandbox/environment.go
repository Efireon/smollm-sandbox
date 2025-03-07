package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"smollm-sandbox/internal/logging"
)

// Константы для песочницы
const (
	TIMEOUT_SECONDS = 30
	MAX_OUTPUT_SIZE = 1024 * 1024 // 1MB
	WORK_DIR        = "/tmp/smollm-sandbox"
)

// Environment представляет песочницу для выполнения кода
type Environment struct {
	logger   *logging.Logger
	workDir  string
	timeouts map[string]int // Таймауты для разных языков
	executor *Executor
}

// CompilerConfig содержит конфигурацию для компилятора
type CompilerConfig struct {
	Command       string
	Args          []string
	OutputFlag    string
	ErrorCodes    map[int]string
	NeedsCompiled bool
}

// ExecutionResult содержит результаты выполнения кода
type ExecutionResult struct {
	Output     string
	Error      string
	ExitCode   int
	ExecuteTime time.Duration
}

// NewEnvironment создает новую песочницу
func NewEnvironment() *Environment {
	logger := logging.NewLogger()
	logger.Info("Initializing sandbox environment")

	// Создаем рабочую директорию
	workDir := WORK_DIR
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		os.MkdirAll(workDir, 0755)
	}

	// Настройка таймаутов для разных языков
	timeouts := map[string]int{
		".py":  TIMEOUT_SECONDS,
		".js":  TIMEOUT_SECONDS,
		".go":  TIMEOUT_SECONDS * 2, // Немного больше времени для компиляции Go
		".c":   TIMEOUT_SECONDS * 2,
		".cpp": TIMEOUT_SECONDS * 2,
	}

	// Создаем исполнитель
	executor := NewExecutor(workDir)

	return &Environment{
		logger:   logger,
		workDir:  workDir,
		timeouts: timeouts,
		executor: executor,
	}
}

// Execute выполняет файл в песочнице
func (e *Environment) Execute(filename string) (string, error) {
	e.logger.Info("Executing file: %s", filename)

	// Выполняем файл через executor
	result, err := e.executor.ExecuteFile(filename)
	if err != nil {
		return "", err
	}

	// Обновляем метрики
	e.logger.GetMetrics().IncrementExecutions()

	// Формируем вывод
	var output string
	if result.Success {
		output = fmt.Sprintf("Выполнение успешно завершено за %v\n\n", result.ExecuteTime)
		output += result.Output
	} else {
		output = fmt.Sprintf("Ошибка выполнения (код %d):\n", result.ExitCode)
		if result.Error != "" {
			output += result.Error + "\n"
		}
		if result.Output != "" {
			output += "\nВывод программы:\n" + result.Output
		}
	}

	return output, nil
}
func (e *Environment) Execute(filename string) (string, error) {
	e.logger.Info("Executing file: %s", filename)

	// Проверяем существование файла
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return "", fmt.Errorf("файл не существует: %s", filename)
	}

	// Определяем тип файла по расширению
	ext := strings.ToLower(filepath.Ext(filename))

	// Получаем конфигурацию для этого типа файла
	config, err := e.getCompilerConfig(ext)
	if err != nil {
		return "", err
	}

	// Копируем файл во временную директорию
	tmpFile := filepath.Join(e.workDir, filepath.Base(filename))
	if err := copyFile(filename, tmpFile); err != nil {
		return "", fmt.Errorf("ошибка копирования файла: %v", err)
	}

	var result ExecutionResult

	// Если нужна компиляция, компилируем
	if config.NeedsCompiled {
		compiledFile, err := e.compile(tmpFile, config)
		if err != nil {
			return "", fmt.Errorf("ошибка компиляции: %v", err)
		}
		tmpFile = compiledFile
	}

	// Выполняем файл
	result, err = e.runFile(tmpFile, config)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения: %v", err)
	}

	// Объединяем вывод и ошибки, если они есть
	output := result.Output
	if result.Error != "" {
		output += "\nErrors:\n" + result.Error
	}

	return output, nil
}

// compile компилирует исходный файл
func (e *Environment) compile(filename string, config CompilerConfig) (string, error) {
	e.logger.Info("Compiling file: %s", filename)

	// Определяем имя выходного файла
	outputFile := strings.TrimSuffix(filename, filepath.Ext(filename))
	
	// Для C/C++ добавляем .out
	if strings.HasSuffix(filename, ".c") || strings.HasSuffix(filename, ".cpp") {
		outputFile += ".out"
	}

	// Подготавливаем аргументы для компилятора
	args := append([]string{}, config.Args...)
	
	// Добавляем выходной файл, если нужно
	if config.OutputFlag != "" {
		args = append(args, config.OutputFlag, outputFile)
	}
	
	// Добавляем имя исходного файла
	args = append(args, filename)

	// Запускаем компилятор
	cmd := exec.Command(config.Command, args...)
	
	// Перенаправляем вывод и ошибки
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// Устанавливаем рабочую директорию
	cmd.Dir = e.workDir
	
	// Устанавливаем таймаут
	timeout := TIMEOUT_SECONDS
	if t, ok := e.timeouts[filepath.Ext(filename)]; ok {
		timeout = t
	}
	
	// Запускаем с таймаутом
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()
	
	// Ждем завершения с таймаутом
	select {
	case err := <-done:
		if err != nil {
			// Проверяем код выхода
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode := exitErr.ExitCode()
				
				// Проверяем наличие известной ошибки
				if errorMsg, ok := config.ErrorCodes[exitCode]; ok {
					return "", fmt.Errorf("%s (код %d): %s", errorMsg, exitCode, stderr.String())
				}
				
				return "", fmt.Errorf("ошибка компиляции (код %d): %s", exitCode, stderr.String())
			}
			
			return "", fmt.Errorf("ошибка запуска компилятора: %v", err)
		}
	case <-time.After(time.Duration(timeout) * time.Second):
		// Убиваем процесс, если он превысил таймаут
		select {
	case err := <-done:
		if err != nil {
			// Проверяем код выхода
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode := exitErr.ExitCode()
				
				// Проверяем наличие известной ошибки
				if errorMsg, ok := config.ErrorCodes[exitCode]; ok {
					return "", fmt.Errorf("%s (код %d): %s", errorMsg, exitCode, stderr.String())
				}
				
				return "", fmt.Errorf("ошибка компиляции (код %d): %s", exitCode, stderr.String())
			}
			
			return "", fmt.Errorf("ошибка запуска компилятора: %v", err)
		}
	case <-time.After(time.Duration(timeout) * time.Second):
		// Убиваем процесс, если он превысил таймаут
		cmd.Process.Kill()
		return "", fmt.Errorf("превышено время компиляции (%d секунд)", timeout)
	}
	
	// Если дошли сюда, значит компиляция прошла успешно
	return outputFile, nil
}