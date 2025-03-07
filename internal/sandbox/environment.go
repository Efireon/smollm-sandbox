package sandbox

import (
	"bytes"
	"errors"
	"fmt"
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
	Output      string
	Error       string
	ExitCode    int
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

// getCompilerConfig возвращает настройки компилятора для указанного расширения файла
func (e *Environment) getCompilerConfig(fileExt string) (CompilerConfig, error) {
	switch strings.ToLower(fileExt) {
	case ".py":
		return CompilerConfig{
			Command:       "python3",
			Args:          []string{"-m", "py_compile"},
			NeedsCompiled: false,
			ErrorCodes:    map[int]string{1: "Синтаксическая ошибка Python"},
		}, nil
	case ".js":
		return CompilerConfig{
			Command:       "node",
			Args:          []string{"--check"},
			NeedsCompiled: false,
			ErrorCodes:    map[int]string{1: "Синтаксическая ошибка JavaScript"},
		}, nil
	case ".go":
		return CompilerConfig{
			Command:       "go",
			Args:          []string{"build"},
			OutputFlag:    "-o",
			NeedsCompiled: true,
			ErrorCodes:    map[int]string{1: "Ошибка компиляции Go"},
		}, nil
	case ".c":
		return CompilerConfig{
			Command:       "gcc",
			Args:          []string{},
			OutputFlag:    "-o",
			NeedsCompiled: true,
			ErrorCodes:    map[int]string{1: "Ошибка компиляции C"},
		}, nil
	case ".cpp":
		return CompilerConfig{
			Command:       "g++",
			Args:          []string{},
			OutputFlag:    "-o",
			NeedsCompiled: true,
			ErrorCodes:    map[int]string{1: "Ошибка компиляции C++"},
		}, nil
	case ".sh":
		return CompilerConfig{
			Command:       "bash",
			Args:          []string{"-n"},
			NeedsCompiled: false,
			ErrorCodes:    map[int]string{1: "Синтаксическая ошибка Bash"},
		}, nil
	default:
		return CompilerConfig{}, fmt.Errorf("неподдерживаемый тип файла: %s", fileExt)
	}
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
		cmd.Process.Kill()
		return "", fmt.Errorf("превышено время компиляции (%d секунд)", timeout)
	}

	// Если дошли сюда, значит компиляция прошла успешно
	return outputFile, nil
}

// runFile запускает файл в песочнице
func (e *Environment) runFile(filename string, config CompilerConfig) (ExecutionResult, error) {
	e.logger.Info("Running file: %s", filename)

	// Определяем команду запуска в зависимости от типа файла
	var cmd *exec.Cmd
	ext := filepath.Ext(filename)

	switch ext {
	case ".py":
		cmd = exec.Command("python3", filename)
	case ".js":
		cmd = exec.Command("node", filename)
	case ".sh":
		cmd = exec.Command("bash", filename)
	default:
		// Для скомпилированных файлов
		if config.NeedsCompiled {
			cmd = exec.Command(filename)
		} else {
			return ExecutionResult{}, fmt.Errorf("неподдерживаемый тип файла: %s", ext)
		}
	}

	// Перенаправляем вывод и ошибки
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Устанавливаем рабочую директорию
	cmd.Dir = e.workDir

	// Устанавливаем ограничения ресурсов
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Создаем новую группу процессов для легкого завершения
	}

	// Устанавливаем таймаут
	timeout := TIMEOUT_SECONDS
	if t, ok := e.timeouts[ext]; ok {
		timeout = t
	}

	// Замеряем время выполнения
	startTime := time.Now()

	// Запускаем с таймаутом
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	var err error
	var exitCode int

	// Ждем завершения с таймаутом
	select {
	case err = <-done:
		// Проверяем код выхода
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}
	case <-time.After(time.Duration(timeout) * time.Second):
		// Убиваем группу процессов, если превышен таймаут
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return ExecutionResult{}, fmt.Errorf("превышено время выполнения (%d секунд)", timeout)
	}

	executeTime := time.Since(startTime)

	// Проверяем размер вывода
	if stdout.Len() > MAX_OUTPUT_SIZE {
		return ExecutionResult{}, fmt.Errorf("превышен максимальный размер вывода (%d байт)", MAX_OUTPUT_SIZE)
	}

	return ExecutionResult{
		Output:      stdout.String(),
		Error:       stderr.String(),
		ExitCode:    exitCode,
		ExecuteTime: executeTime,
	}, nil
}

// ExecuteCode выполняет строку кода указанного языка
func (e *Environment) ExecuteCode(code string, language string) (string, error) {
	e.logger.Info("Executing code snippet in language: %s", language)

	// Выполняем код через executor
	result, err := e.executor.ExecuteCode(code, language)
	if err != nil {
		return "", err
	}

	// Обновляем метрики
	e.logger.GetMetrics().IncrementExecutions()

	// Формируем вывод
	var output string
	if result.Success {
		output = fmt.Sprintf("Выполнение успешно завершено за %v\n\n", result.ExecuteTime)
		if result.Compiled {
			output += fmt.Sprintf("Время компиляции: %v\n", result.CompileTime)
		}
		output += result.Output
	} else {
		if result.Compiled && !result.Success {
			output = "Ошибка компиляции:\n" + result.Error
		} else {
			output = fmt.Sprintf("Ошибка выполнения (код %d):\n", result.ExitCode)
			if result.Error != "" {
				output += result.Error + "\n"
			}
			if result.Output != "" {
				output += "\nВывод программы:\n" + result.Output
			}
		}
	}

	return output, nil
}

// GetSupportedLanguages возвращает список поддерживаемых языков
func (e *Environment) GetSupportedLanguages() []string {
	return []string{
		"python",
		"javascript",
		"go",
		"c",
		"c++",
		"bash",
	}
}

// CheckFileSecurity проверяет безопасность файла
func (e *Environment) CheckFileSecurity(filename string) error {
	// Проверяем существование файла
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return fmt.Errorf("файл не существует: %s", filename)
	}

	// Проверяем размер файла
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return err
	}

	// Максимальный размер файла (10MB)
	maxSize := int64(10 * 1024 * 1024)
	if fileInfo.Size() > maxSize {
		return fmt.Errorf("превышен максимальный размер файла: %d байт (максимум %d)", fileInfo.Size(), maxSize)
	}

	// Проверяем расширение файла
	ext := strings.ToLower(filepath.Ext(filename))
	supportedExts := map[string]bool{
		".py":   true,
		".js":   true,
		".go":   true,
		".c":    true,
		".cpp":  true,
		".sh":   true,
		".txt":  true,
		".md":   true,
		".json": true,
		".csv":  true,
	}

	if !supportedExts[ext] {
		return fmt.Errorf("неподдерживаемый тип файла: %s", ext)
	}

	// Проверяем на подозрительное содержимое
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// Простая проверка на подозрительные паттерны
	suspiciousPatterns := []string{
		"system(", "exec(", "fork(", "subprocess",
		"socket", "urllib", "requests", "http",
		"child_process", "spawn", "shellexec",
		"wget", "curl", "rm -rf", "sudo",
	}

	content := strings.ToLower(string(data))
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(content, pattern) {
			e.logger.Warn("Suspicious pattern found in file %s: %s", filename, pattern)
			// Мы не блокируем файл, а только логируем подозрительные паттерны
		}
	}

	return nil
}

// SetResourceLimits устанавливает ограничения ресурсов для песочницы
func (e *Environment) SetResourceLimits(cpuPercent int, memoryMB int, timeoutSeconds int) {
	e.logger.Info("Setting resource limits: CPU=%d%%, Memory=%dMB, Timeout=%ds",
		cpuPercent, memoryMB, timeoutSeconds)

	// Обновляем таймауты для всех языков
	for ext := range e.timeouts {
		e.timeouts[ext] = timeoutSeconds
	}

	// TODO: Реализовать ограничения на CPU и память
	// Это может потребовать использования cgroups в Linux
}

// CleanupTempFiles очищает временные файлы в рабочей директории
func (e *Environment) CleanupTempFiles() error {
	e.logger.Info("Cleaning up temporary files in %s", e.workDir)

	// Читаем все файлы в рабочей директории
	files, err := os.ReadDir(e.workDir)
	if err != nil {
		return fmt.Errorf("ошибка чтения директории: %v", err)
	}

	// Удаляем временные файлы
	var errors []string
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "tmp_") {
			path := filepath.Join(e.workDir, file.Name())
			if err := os.Remove(path); err != nil {
				errors = append(errors, fmt.Sprintf("не удалось удалить %s: %v", file.Name(), err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("ошибки при очистке: %s", strings.Join(errors, "; "))
	}

	return nil
}

// GetWorkDir возвращает рабочую директорию песочницы
func (e *Environment) GetWorkDir() string {
	return e.workDir
}

// GetTimeout возвращает таймаут для указанного расширения файла
func (e *Environment) GetTimeout(fileExt string) int {
	if timeout, ok := e.timeouts[fileExt]; ok {
		return timeout
	}
	return TIMEOUT_SECONDS
}
