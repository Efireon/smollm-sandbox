package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Константы для уровней логирования
const (
	DEBUG = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Logger представляет систему логирования
type Logger struct {
	level      int
	output     io.Writer
	fileOutput *os.File
	mu         sync.Mutex
	metrics    *Metrics
}

// LogConfig содержит настройки логирования
type LogConfig struct {
	Level         int    // Уровень логирования
	EnableFile    bool   // Записывать ли логи в файл
	FilePath      string // Путь к файлу логов
	EnableConsole bool   // Выводить ли логи в консоль
}

// DefaultLogConfig возвращает настройки по умолчанию
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:         INFO,
		EnableFile:    true,
		FilePath:      "logs/smollm.log",
		EnableConsole: true,
	}
}

// NewLogger создает новый экземпляр логгера
func NewLogger() *Logger {
	return NewLoggerWithConfig(DefaultLogConfig())
}

// NewLoggerWithConfig создает новый экземпляр логгера с указанными настройками
func NewLoggerWithConfig(config LogConfig) *Logger {
	var output io.Writer = os.Stdout
	var fileOutput *os.File

	// Если включена запись в файл
	if config.EnableFile {
		// Создаем директорию для логов, если нужно
		logDir := filepath.Dir(config.FilePath)
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			os.MkdirAll(logDir, 0755)
		}

		// Открываем файл для записи (с добавлением)
		file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Failed to open log file: %v", err)
		} else {
			fileOutput = file

			// Если нужно выводить и в консоль, и в файл
			if config.EnableConsole {
				output = io.MultiWriter(os.Stdout, file)
			} else {
				output = file
			}
		}
	}

	return &Logger{
		level:      config.Level,
		output:     output,
		fileOutput: fileOutput,
		metrics:    NewMetrics(),
	}
}

// Close закрывает ресурсы логгера
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fileOutput != nil {
		l.fileOutput.Close()
		l.fileOutput = nil
	}
}

// SetLevel устанавливает уровень логирования
func (l *Logger) SetLevel(level int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Debug логирует отладочное сообщение
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Info логирует информационное сообщение
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warn логирует предупреждение
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Error логирует ошибку
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// Fatal логирует критическую ошибку и завершает программу
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
	os.Exit(1)
}

// log записывает сообщение с указанным уровнем
func (l *Logger) log(level int, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// Получаем информацию о вызывающем файле и строке
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		file = "???"
		line = 0
	}

	// Для краткости используем только имя файла
	file = filepath.Base(file)

	// Формируем префикс лога
	levelStr := "UNKNOWN"
	switch level {
	case DEBUG:
		levelStr = "DEBUG"
	case INFO:
		levelStr = "INFO"
	case WARN:
		levelStr = "WARN"
	case ERROR:
		levelStr = "ERROR"
	case FATAL:
		levelStr = "FATAL"
	}

	// Форматируем время
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	// Форматируем сообщение
	message := fmt.Sprintf(format, args...)

	// Записываем лог
	fmt.Fprintf(l.output, "[%s] [%s] [%s:%d] %s\n", timestamp, levelStr, file, line, message)

	// Обновляем метрики
	l.metrics.IncrementLogCount(level)
	if level >= ERROR {
		l.metrics.IncrementErrorCount()
	}
}

// GetMetrics возвращает метрики логирования
func (l *Logger) GetMetrics() *Metrics {
	return l.metrics
}
