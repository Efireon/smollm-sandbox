package main

import (
	"flag"
	"log"
	"path/filepath"

	"smollm-sandbox/internal/feedback"
	"smollm-sandbox/internal/logging"
	"smollm-sandbox/internal/model"
	"smollm-sandbox/internal/sandbox"
	"smollm-sandbox/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	VERSION = "0.1.0"
)

var (
	logger        *logging.Logger
	modelInstance *model.SmolLM
	sandboxEnv    *sandbox.Environment
	store         *storage.FileSystem
	collector     *feedback.Collector
	configPath    string
	token         string
	allowedUsers  []int64
	adminUsers    []int64
)

func main() {
	// Определение флагов
	flag.StringVar(&configPath, "config", "configs/config.yaml", "Путь к файлу конфигурации")
	flag.StringVar(&token, "token", "", "Токен Telegram бота")
	flag.Parse()

	// Проверка токена
	if token == "" {
		log.Fatal("Не указан токен Telegram бота")
	}

	// Инициализация логирования
	logger = logging.NewLogger()
	logger.Info("Starting SmolLM Telegram Bot")

	// Загрузка конфигурации
	logger.Info("Loading configuration from %s", configPath)
	// TODO: Реализовать загрузку конфигурации

	// Инициализация хранилища
	homeDir := getHomeDir()
	store = storage.NewFileSystem(homeDir)

	// Инициализация модели
	logger.Info("Initializing SmolLM2 model")
	modelInstance = model.NewSmolLM()

	// Инициализация песочницы
	logger.Info("Setting up sandbox environment")
	sandboxEnv = sandbox.NewEnvironment()

	// Инициализация сборщика обратной связи
	feedbackDir := filepath.Join(homeDir, "feedback")
	collector = feedback.NewCollector(feedbackDir)

	// Инициализация бота
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	logger.Info("Authorized on account %s", bot.Self.UserName)

	// Установка обработчика обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Обработка сообщений
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Проверка доступа пользователя
		if !isUserAllowed(update.Message.From.ID) {
			logger.Warn("Unauthorized access attempt from user %d", update.Message.From.ID)
			continue
		}

		go handleMessage(bot, update.Message)
	}
}
