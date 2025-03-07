package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"smollm-sandbox/internal/logging"
)

// FileSystem предоставляет интерфейс для работы с файловой системой
type FileSystem struct {
	logger      *logging.Logger
	rootDir     string
	sessionDir  string
	thoughtsDir string
	codeDir     string
	tempDir     string
}

// FileInfo содержит информацию о файле
type FileInfo struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	IsDir      bool      `json:"is_dir"`
	Permission string    `json:"permission"`
}

// SessionInfo содержит информацию о сессии
type SessionInfo struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
}

// NewFileSystem создает новый экземпляр FileSystem
func NewFileSystem(rootDir string) *FileSystem {
	logger := logging.NewLogger()

	// Создаем основные директории, если они не существуют
	sessionDir := filepath.Join(rootDir, "sessions")
	thoughtsDir := filepath.Join(rootDir, "thoughts")
	codeDir := filepath.Join(rootDir, "code")
	tempDir := filepath.Join(rootDir, "temp")

	dirs := []string{rootDir, sessionDir, thoughtsDir, codeDir, tempDir}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Error("Failed to create directory %s: %v", dir, err)
			}
		}
	}

	return &FileSystem{
		logger:      logger,
		rootDir:     rootDir,
		sessionDir:  sessionDir,
		thoughtsDir: thoughtsDir,
		codeDir:     codeDir,
		tempDir:     tempDir,
	}
}

// CreateFile создает новый файл и возвращает путь к нему
func (fs *FileSystem) CreateFile(dir, name string) (string, error) {
	// Проверяем, что директория существует внутри нашего корня
	fullDir := filepath.Join(fs.rootDir, dir)
	if !fs.isPathSafe(fullDir) {
		return "", errors.New("путь находится за пределами разрешенной директории")
	}

	// Создаем директорию, если она не существует
	if _, err := os.Stat(fullDir); os.IsNotExist(err) {
		if err := os.MkdirAll(fullDir, 0755); err != nil {
			return "", err
		}
	}

	// Путь к новому файлу
	filePath := filepath.Join(fullDir, name)

	// Создаем пустой файл
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return filePath, nil
}

// WriteFile записывает данные в файл
func (fs *FileSystem) WriteFile(path string, data []byte) error {
	// Проверяем, что путь находится внутри нашего корня
	if !fs.isPathSafe(path) {
		return errors.New("путь находится за пределами разрешенной директории")
	}

	return os.WriteFile(path, data, 0644)
}

// ReadFile читает данные из файла
func (fs *FileSystem) ReadFile(path string) ([]byte, error) {
	// Проверяем, что путь находится внутри нашего корня
	if !fs.isPathSafe(path) {
		return nil, errors.New("путь находится за пределами разрешенной директории")
	}

	return os.ReadFile(path)
}

// DeleteFile удаляет файл
func (fs *FileSystem) DeleteFile(path string) error {
	// Проверяем, что путь находится внутри нашего корня
	if !fs.isPathSafe(path) {
		return errors.New("путь находится за пределами разрешенной директории")
	}

	return os.Remove(path)
}

// ListFiles возвращает список файлов в указанной директории
func (fs *FileSystem) ListFiles(dir string) ([]FileInfo, error) {
	// Проверяем, что директория существует внутри нашего корня
	fullDir := filepath.Join(fs.rootDir, dir)
	if !fs.isPathSafe(fullDir) {
		return nil, errors.New("путь находится за пределами разрешенной директории")
	}

	// Проверяем существование директории
	if _, err := os.Stat(fullDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("директория не существует: %s", dir)
	}

	// Читаем содержимое директории
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return nil, err
	}

	// Подготавливаем результат
	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			fs.logger.Warn("Failed to get info for %s: %v", entry.Name(), err)
			continue
		}

		path := filepath.Join(fullDir, entry.Name())

		fileInfo := FileInfo{
			Name:    entry.Name(),
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		}

		// Определяем права доступа
		if info.Mode().IsRegular() {
			if info.Mode()&0111 != 0 {
				fileInfo.Permission = "executable"
			} else {
				fileInfo.Permission = "readable"
			}
		} else if info.Mode().IsDir() {
			fileInfo.Permission = "directory"
		}

		result = append(result, fileInfo)
	}

	// Сортируем по имени
	sort.Slice(result, func(i, j int) bool {
		// Сначала директории, затем файлы
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// CopyFile копирует файл из источника в назначение
func (fs *FileSystem) CopyFile(src, dst string) error {
	// Проверяем, что оба пути находятся внутри нашего корня
	if !fs.isPathSafe(src) || !fs.isPathSafe(dst) {
		return errors.New("путь находится за пределами разрешенной директории")
	}

	// Проверяем существование исходного файла
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		return errors.New("копирование директорий не поддерживается")
	}

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

	// Копируем права доступа
	return os.Chmod(dst, srcInfo.Mode())
}

// SaveSession сохраняет сессию
func (fs *FileSystem) SaveSession(name string, data []byte) error {
	sessionPath := filepath.Join(fs.sessionDir, name+".json")
	return fs.WriteFile(sessionPath, data)
}

// LoadSession загружает сессию
func (fs *FileSystem) LoadSession(name string) ([]byte, error) {
	sessionPath := filepath.Join(fs.sessionDir, name+".json")
	return fs.ReadFile(sessionPath)
}

// ListSessions возвращает список доступных сессий
func (fs *FileSystem) ListSessions() ([]SessionInfo, error) {
	// Проверяем существование директории с сессиями
	if _, err := os.Stat(fs.sessionDir); os.IsNotExist(err) {
		return nil, errors.New("директория сессий не существует")
	}

	// Читаем содержимое директории
	entries, err := os.ReadDir(fs.sessionDir)
	if err != nil {
		return nil, err
	}

	// Подготавливаем результат
	result := make([]SessionInfo, 0, len(entries))
	for _, entry := range entries {
		// Пропускаем не JSON файлы
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			info, err := entry.Info()
			if err != nil {
				fs.logger.Warn("Failed to get info for %s: %v", entry.Name(), err)
				continue
			}

			path := filepath.Join(fs.sessionDir, entry.Name())
			name := entry.Name()[:len(entry.Name())-len(".json")]

			sessionInfo := SessionInfo{
				Name:       name,
				Path:       path,
				CreatedAt:  info.ModTime(),
				ModifiedAt: info.ModTime(),
				Size:       info.Size(),
			}

			result = append(result, sessionInfo)
		}
	}

	// Сортируем по времени изменения (от новых к старым)
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModifiedAt.After(result[j].ModifiedAt)
	})

	return result, nil
}

// isPathSafe проверяет, что путь находится внутри корневой директории
func (fs *FileSystem) isPathSafe(path string) bool {
	// Получаем абсолютные пути
	absRoot, err := filepath.Abs(fs.rootDir)
	if err != nil {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Проверяем, что путь начинается с корневой директории
	return strings.HasPrefix(absPath, absRoot)
}
