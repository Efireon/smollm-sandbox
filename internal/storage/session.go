package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smollm-sandbox/internal/logging"
	"smollm-sandbox/internal/model"
)

// SessionManager управляет сессиями нейросети
type SessionManager struct {
	logger      *logging.Logger
	fs          *FileSystem
	sessionsDir string
}

// SessionMeta представляет метаинформацию о сессии
type SessionMeta struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// NewSessionManager создает новый экземпляр SessionManager
func NewSessionManager(fs *FileSystem, sessionsDir string) *SessionManager {
	logger := logging.NewLogger()

	// Создаем директорию для сессий, если нужно
	fullPath := filepath.Join(fs.rootDir, sessionsDir)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			logger.Error("Failed to create sessions directory: %v", err)
		}
	}

	return &SessionManager{
		logger:      logger,
		fs:          fs,
		sessionsDir: sessionsDir,
	}
}

// SaveSession сохраняет контекст сессии
func (sm *SessionManager) SaveSession(name string, context *model.Context) error {
	// Проверяем название сессии на допустимые символы
	if !isValidSessionName(name) {
		return errors.New("недопустимое имя сессии (разрешены только буквы, цифры, дефисы и подчеркивания)")
	}

	// Сериализуем контекст в JSON
	data, err := context.ToJSON()
	if err != nil {
		return fmt.Errorf("ошибка сериализации контекста: %v", err)
	}

	// Создаем путь к файлу сессии
	sessionPath := filepath.Join(sm.sessionsDir, name+".json")

	// Сохраняем файл
	if err := sm.fs.WriteFile(sessionPath, data); err != nil {
		return fmt.Errorf("ошибка записи файла сессии: %v", err)
	}

	sm.logger.Info("Сессия успешно сохранена: %s", name)
	return nil
}

// LoadSession загружает контекст сессии
func (sm *SessionManager) LoadSession(name string) (*model.Context, error) {
	// Проверяем название сессии
	if !isValidSessionName(name) {
		return nil, errors.New("недопустимое имя сессии")
	}

	// Формируем путь к файлу сессии
	sessionPath := filepath.Join(sm.sessionsDir, name+".json")

	// Загружаем файл
	data, err := sm.fs.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла сессии: %v", err)
	}

	// Десериализуем контекст из JSON
	context := model.NewContext()
	if err := context.FromJSON(data); err != nil {
		return nil, fmt.Errorf("ошибка десериализации контекста: %v", err)
	}

	sm.logger.Info("Сессия успешно загружена: %s", name)
	return context, nil
}

// DeleteSession удаляет сессию
func (sm *SessionManager) DeleteSession(name string) error {
	// Проверяем название сессии
	if !isValidSessionName(name) {
		return errors.New("недопустимое имя сессии")
	}

	// Формируем путь к файлу сессии
	sessionPath := filepath.Join(sm.sessionsDir, name+".json")

	// Удаляем файл
	if err := sm.fs.DeleteFile(sessionPath); err != nil {
		return fmt.Errorf("ошибка удаления файла сессии: %v", err)
	}

	sm.logger.Info("Сессия успешно удалена: %s", name)
	return nil
}

// ListSessions возвращает список всех сессий
func (sm *SessionManager) ListSessions() ([]SessionMeta, error) {
	// Получаем список файлов в директории сессий
	files, err := sm.fs.ListFiles(sm.sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения директории сессий: %v", err)
	}

	// Фильтруем и собираем информацию о сессиях
	sessions := make([]SessionMeta, 0, len(files))
	for _, file := range files {
		// Проверяем, что это JSON файл
		if !strings.HasSuffix(file.Name, ".json") {
			continue
		}

		// Извлекаем имя сессии из имени файла
		name := strings.TrimSuffix(file.Name, ".json")

		// Читаем файл для получения дополнительной информации
		data, err := sm.fs.ReadFile(filepath.Join(sm.sessionsDir, file.Name))
		if err != nil {
			sm.logger.Warn("Не удалось прочитать файл сессии %s: %v", file.Name, err)
			continue
		}

		// Парсим частично, чтобы получить метаданные
		var contextData map[string]interface{}
		if err := json.Unmarshal(data, &contextData); err != nil {
			sm.logger.Warn("Не удалось разобрать файл сессии %s: %v", file.Name, err)
			continue
		}

		// Извлекаем информацию о сессии
		var createdAt, updatedAt time.Time
		var messageCount int

		if metadata, ok := contextData["metadata"].(map[string]interface{}); ok {
			if createdAtStr, ok := metadata["created_at"].(string); ok {
				createdAt, _ = time.Parse(time.RFC3339, createdAtStr)
			}
			if updatedAtStr, ok := metadata["updated_at"].(string); ok {
				updatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
			}
		}

		if messages, ok := contextData["messages"].([]interface{}); ok {
			messageCount = len(messages)
		}

		// Добавляем в результат
		sessions = append(sessions, SessionMeta{
			Name:         name,
			Path:         filepath.Join(sm.sessionsDir, file.Name),
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
			MessageCount: messageCount,
		})
	}

	return sessions, nil
}

// isValidSessionName проверяет допустимость имени сессии
func isValidSessionName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}

	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return false
		}
	}

	return true
}
