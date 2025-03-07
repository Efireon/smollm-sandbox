package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"smollm-sandbox/internal/logging"
)

// Compiler обеспечивает компиляцию и проверку кода
type Compiler struct {
	logger  *logging.Logger
	workDir string
	configs map[string]CompilerConfig
}

// CompileResult содержит результаты компиляции
type CompileResult struct {
	Success     bool
	OutputFile  string
	Error       string
	CompileTime time.Duration
}

// NewCompiler создает новый экземпляр компилятора
func NewCompiler(workDir string) *Compiler {
	logger := logging.NewLogger()

	// Создаем рабочую директорию, если ее нет
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		os.MkdirAll(workDir, 0755)
	}

	// Инициализируем конфигурации для разных языков
	configs := make(map[string]CompilerConfig)

	// Python
	configs[".py"] = CompilerConfig{
		Command:       "python3",
		Args:          []string{"-m", "py_compile"},
		NeedsCompiled: false,
		ErrorCodes:    map[int]string{1: "Синтаксическая ошибка Python"},
	}

	// JavaScript
	configs[".js"] = CompilerConfig{
		Command:       "node",
		Args:          []string{"--check"},
		NeedsCompiled: false,
		ErrorCodes:    map[int]string{1: "Синтаксическая ошибка JavaScript"},
	}

	// Go
	configs[".go"] = CompilerConfig{
		Command:       "go",
		Args:          []string{"build"},
		OutputFlag:    "-o",
		NeedsCompiled: true,
		ErrorCodes:    map[int]string{1: "Ошибка компиляции Go"},
	}

	// C
	configs[".c"] = CompilerConfig{
		Command:       "gcc",
		Args:          []string{"-Wall", "-O2"},
		OutputFlag:    "-o",
		NeedsCompiled: true,
		ErrorCodes:    map[int]string{1: "Ошибка компиляции C"},
	}

	// C++
	configs[".cpp"] = CompilerConfig{
		Command:       "g++",
		Args:          []string{"-Wall", "-O2", "-std=c++17"},
		OutputFlag:    "-o",
		NeedsCompiled: true,
		ErrorCodes:    map[int]string{1: "Ошибка компиляции C++"},
	}

	// Bash
	configs[".sh"] = CompilerConfig{
		Command:       "bash",
		Args:          []string{"-n"},
		NeedsCompiled: false,
		ErrorCodes:    map[int]string{1: "Синтаксическая ошибка Bash"},
	}

	return &Compiler{
		logger:  logger,
		workDir: workDir,
		configs: configs,
	}
}

// Compile компилирует исходный код
func (c *Compiler) Compile(sourcePath string) (*CompileResult, error) {
	c.logger.Info("Compiling: %s", sourcePath)

	// Получаем расширение файла
	ext := strings.ToLower(filepath.Ext(sourcePath))

	// Проверяем наличие конфигурации для этого типа файла
	config, ok := c.configs[ext]
	if !ok {
		return nil, fmt.Errorf("неподдерживаемый тип файла: %s", ext)
	}

	// Формируем имя выходного файла
	outputFile := strings.TrimSuffix(sourcePath, ext)

	// Для C/C++ добавляем .out
	if ext == ".c" || ext == ".cpp" {
		outputFile += ".out"
	}

	// Формируем команду компиляции
	var cmd *exec.Cmd

	if config.NeedsCompiled {
		// Подготавливаем аргументы
		args := append([]string{}, config.Args...)

		// Добавляем флаг выходного файла и сам выходной файл
		if config.OutputFlag != "" {
			args = append(args, config.OutputFlag, outputFile)
		}

		// Добавляем исходный файл
		args = append(args, sourcePath)

		cmd = exec.Command(config.Command, args...)
	} else {
		// Для интерпретируемых языков, просто проверяем синтаксис
		args := append([]string{}, config.Args...)
		args = append(args, sourcePath)
		cmd = exec.Command(config.Command, args...)
	}

	// Перенаправляем вывод
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Устанавливаем рабочую директорию
	cmd.Dir = c.workDir

	// Измеряем время выполнения
	startTime := time.Now()

	// Выполняем компиляцию
	err := cmd.Run()
	compileTime := time.Since(startTime)

	// Проверяем результат
	if err != nil {
		// Проверяем код выхода
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()

			// Проверяем наличие известной ошибки
			errorMsg := stderr.String()
			if customMsg, ok := config.ErrorCodes[exitCode]; ok {
				errorMsg = fmt.Sprintf("%s: %s", customMsg, errorMsg)
			}

			return &CompileResult{
				Success:     false,
				Error:       errorMsg,
				CompileTime: compileTime,
			}, nil
		}

		return &CompileResult{
			Success:     false,
			Error:       fmt.Sprintf("ошибка запуска компилятора: %v", err),
			CompileTime: compileTime,
		}, nil
	}

	// Для скомпилированных файлов проверяем наличие выходного файла
	if config.NeedsCompiled {
		if _, err := os.Stat(outputFile); os.IsNotExist(err) {
			return &CompileResult{
				Success:     false,
				Error:       "компиляция не создала выходной файл",
				CompileTime: compileTime,
			}, nil
		}

		// Устанавливаем права на выполнение
		os.Chmod(outputFile, 0755)
	}

	return &CompileResult{
		Success:     true,
		OutputFile:  outputFile,
		CompileTime: compileTime,
	}, nil
}

// CheckSyntax проверяет синтаксис кода
func (c *Compiler) CheckSyntax(sourcePath string) (bool, string, error) {
	// Получаем расширение файла
	ext := strings.ToLower(filepath.Ext(sourcePath))

	// Проверяем наличие конфигурации для этого типа файла
	config, ok := c.configs[ext]
	if !ok {
		return false, "", fmt.Errorf("неподдерживаемый тип файла: %s", ext)
	}

	// Формируем команду проверки синтаксиса
	var cmd *exec.Cmd

	if config.NeedsCompiled {
		// Для компилируемых языков используем флаги только для проверки
		switch ext {
		case ".go":
			cmd = exec.Command("go", "vet", sourcePath)
		case ".c", ".cpp":
			cmd = exec.Command(config.Command, "-fsyntax-only", sourcePath)
		default:
			// Если нет специального флага для проверки, используем обычную компиляцию
			args := append([]string{}, config.Args...)
			args = append(args, "-fsyntax-only", sourcePath)
			cmd = exec.Command(config.Command, args...)
		}
	} else {
		// Для интерпретируемых языков используем штатные средства
		args := append([]string{}, config.Args...)
		args = append(args, sourcePath)
		cmd = exec.Command(config.Command, args...)
	}

	// Перенаправляем вывод
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Выполняем проверку
	err := cmd.Run()

	// Анализируем результат
	if err != nil {
		return false, stderr.String(), nil
	}

	return true, "", nil
}
