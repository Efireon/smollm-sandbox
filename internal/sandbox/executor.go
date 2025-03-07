package sandbox

import (
	"bytes"
	"context"
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

// Executor обеспечивает безопасное выполнение кода
type Executor struct {
	logger        *logging.Logger
	workDir       string
	maxOutputSize int
	timeouts      map[string]int
	compiler      *Compiler
}

// ExecuteResult содержит результаты выполнения кода
type ExecuteResult struct {
	Success     bool
	Output      string
	Error       string
	ExitCode    int
	ExecuteTime time.Duration
	CompileTime time.Duration
	Compiled    bool
	Language    string
}

// NewExecutor создает новый экземпляр исполнителя
func NewExecutor(workDir string) *Executor {
	logger := logging.NewLogger()

	// Создаем рабочую директорию, если нужно
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		os.MkdirAll(workDir, 0755)
	}

	// Настройка таймаутов для разных языков
	timeouts := map[string]int{
		".py":  TIMEOUT_SECONDS,
		".js":  TIMEOUT_SECONDS,
		".go":  TIMEOUT_SECONDS * 2, // Больше времени для Go
		".c":   TIMEOUT_SECONDS * 2,
		".cpp": TIMEOUT_SECONDS * 2,
		".sh":  TIMEOUT_SECONDS,
	}

	return &Executor{
		logger:        logger,
		workDir:       workDir,
		maxOutputSize: MAX_OUTPUT_SIZE,
		timeouts:      timeouts,
		compiler:      NewCompiler(workDir),
	}
}

// ExecuteFile выполняет указанный файл
func (e *Executor) ExecuteFile(filePath string) (*ExecuteResult, error) {
	e.logger.Info("Executing file: %s", filePath)

	// Проверяем существование файла
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл не существует: %s", filePath)
	}

	// Определяем тип файла по расширению
	ext := strings.ToLower(filepath.Ext(filePath))

	// Копируем файл во временную директорию
	baseName := filepath.Base(filePath)
	tempFile := filepath.Join(e.workDir, baseName)

	if err := copyFile(filePath, tempFile); err != nil {
		return nil, fmt.Errorf("ошибка копирования файла: %v", err)
	}

	// Исполняемый файл
	executablePath := tempFile
	var compileResult *CompileResult
	var err error

	// Если файл нуждается в компиляции, компилируем его
	if ext == ".c" || ext == ".cpp" || ext == ".go" {
		compileResult, err = e.compiler.Compile(tempFile)
		if err != nil {
			return nil, fmt.Errorf("ошибка компиляции: %v", err)
		}

		if !compileResult.Success {
			return &ExecuteResult{
				Success:     false,
				Error:       compileResult.Error,
				CompileTime: compileResult.CompileTime,
				Compiled:    true,
				Language:    ext,
			}, nil
		}

		// Используем скомпилированный файл
		executablePath = compileResult.OutputFile
	}

	// Устанавливаем таймаут
	timeout := TIMEOUT_SECONDS
	if t, ok := e.timeouts[ext]; ok {
		timeout = t
	}

	// Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Готовим команду для выполнения
	var cmd *exec.Cmd

	switch ext {
	case ".py":
		cmd = exec.Command("python3", executablePath)
	case ".js":
		cmd = exec.Command("node", executablePath)
	case ".sh":
		cmd = exec.Command("bash", executablePath)
	case ".c", ".cpp", ".go":
		cmd = exec.Command(executablePath)
	default:
		return nil, fmt.Errorf("неподдерживаемый тип файла: %s", ext)
	}

	// Настраиваем ограничения
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Создаем новую группу процессов
	}

	// Буферы для вывода
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Рабочая директория
	cmd.Dir = e.workDir

	// Запускаем процесс
	startTime := time.Now()
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("ошибка запуска процесса: %v", err)
	}

	// Завершение процесса с обработкой таймаута
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- cmd.Wait()
	}()

	// Ожидаем завершения или таймаута
	var executeErr error
	select {
	case <-ctx.Done():
		// Превышен таймаут, убиваем процесс
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		executeErr = fmt.Errorf("превышено время выполнения (%d секунд)", timeout)
	case err := <-doneCh:
		executeErr = err
	}

	// Измеряем время выполнения
	executeTime := time.Since(startTime)

	// Проверяем размер вывода
	if stdout.Len() > e.maxOutputSize {
		executeErr = fmt.Errorf("превышен максимальный размер вывода (%d байт)", e.maxOutputSize)
	}

	// Анализируем результат
	exitCode := 0
	success := true

	if executeErr != nil {
		success = false
		if exitErr, ok := executeErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Формируем результат
	result := &ExecuteResult{
		Success:     success,
		Output:      stdout.String(),
		Error:       stderr.String(),
		ExitCode:    exitCode,
		ExecuteTime: executeTime,
		Language:    ext,
	}

	// Если была компиляция, добавляем информацию о ней
	if compileResult != nil {
		result.Compiled = true
		result.CompileTime = compileResult.CompileTime
	}

	// Логируем результат
	e.logger.Info("Execution completed: success=%v, exit_code=%d, time=%v",
		result.Success, result.ExitCode, result.ExecuteTime)

	return result, nil
}

// copyFile копирует файл из источника в назначение
func copyFile(src, dst string) error {
	// Открываем исходный файл
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Создаем целевой файл
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Копируем содержимое
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Устанавливаем права на выполнение
	return os.Chmod(dst, 0755)
}
func (e *Executor) ExecuteCode(code string, language string) (*ExecuteResult, error) {
	// Определяем расширение файла на основе языка
	var ext string
	switch strings.ToLower(language) {
	case "python":
		ext = ".py"
	case "javascript", "js":
		ext = ".js"
	case "go", "golang":
		ext = ".go"
	case "c":
		ext = ".c"
	case "cpp", "c++":
		ext = ".cpp"
	case "bash", "sh":
		ext = ".sh"
	default:
		return nil, fmt.Errorf("неподдерживаемый язык: %s", language)
	}

	// Создаем временный файл для кода
	timestamp := time.Now().UnixNano()
	fileName := fmt.Sprintf("tmp_%d%s", timestamp, ext)
	filePath := filepath.Join(e.workDir, fileName)

	// Записываем код во временный файл
	if err := os.WriteFile(filePath, []byte(code), 0755); err != nil {
		return nil, fmt.Errorf("ошибка записи во временный файл: %v", err)
	}

	// Выполняем файл
	result, err := e.ExecuteFile(filePath)

	// Удаляем временный файл
	os.Remove(filePath)

	return result, err
}
