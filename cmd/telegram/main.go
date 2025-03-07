package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

// getHomeDir возвращает домашнюю директорию
func getHomeDir() string {
	// В продакшне здесь будет домашняя директория учетной записи нейросети
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("Не удалось определить домашнюю директорию: %v", err)
		return "/tmp/smollm-sandbox"
	}
	return filepath.Join(homeDir, ".smollm-sandbox")
}

// isUserAllowed проверяет, разрешен ли доступ пользователю
func isUserAllowed(userID int64) bool {
	// Если список разрешенных пуст, разрешаем всем
	if len(allowedUsers) == 0 {
		return true
	}

	// Проверяем, есть ли пользователь в списке разрешенных
	for _, id := range allowedUsers {
		if id == userID {
			return true
		}
	}

	// Проверяем, является ли пользователь администратором
	for _, id := range adminUsers {
		if id == userID {
			return true
		}
	}

	return false
}

// handleMessage обрабатывает входящее сообщение
func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	logger.Info("Received message from %d: %s", message.From.ID, message.Text)

	// Проверка на команды
	if message.IsCommand() {
		handleCommand(bot, message)
		return
	}

	// Обработка обычного текста
	response := modelInstance.Process(message.Text)

	// Отправляем ответ
	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	msg.ReplyToMessageID = message.MessageID

	if _, err := bot.Send(msg); err != nil {
		logger.Error("Failed to send message: %v", err)
	}
}

// handleCommand обрабатывает команды бота
func handleCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	switch message.Command() {
	case "start":
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Привет! Я SmolLM бот v%s. Напиши мне что-нибудь, и я отвечу.", VERSION))
		bot.Send(msg)

	case "help":
		helpText := `
Команды бота:
/start - Начать общение
/help - Показать справку
/think [время] - Запустить режим размышления
/run - Выполнить код (отправь код в следующем сообщении)
/status - Показать статус бота
/feedback [оценка] [комментарий] - Отправить обратную связь
`
		msg := tgbotapi.NewMessage(message.Chat.ID, helpText)
		bot.Send(msg)

	case "think":
		// Разбираем аргументы
		args := message.CommandArguments()
		seconds := 60 // По умолчанию
		if args != "" {
			fmt.Sscanf(args, "%d", &seconds)
		}

		// Ограничиваем время
		if seconds < 10 {
			seconds = 10
		} else if seconds > 300 {
			seconds = 300
		}

		// Отправляем начальное сообщение
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Запускаю режим размышления на %d секунд...", seconds))
		bot.Send(msg)

		// Создаем временный файл для мыслей
		timestamp := time.Now().Format("20060102_150405")
		thoughtFile := filepath.Join(getHomeDir(), "thoughts", fmt.Sprintf("tg_thought_%s.txt", timestamp))

		// Создаем директорию, если нужно
		os.MkdirAll(filepath.Dir(thoughtFile), 0755)

		// Запускаем размышление
		modelInstance.Think(seconds, thoughtFile)

		// Читаем файл
		thoughts, err := os.ReadFile(thoughtFile)
		if err != nil {
			logger.Error("Failed to read thoughts file: %v", err)
			msg = tgbotapi.NewMessage(message.Chat.ID, "Произошла ошибка при чтении файла размышлений.")
			bot.Send(msg)
			return
		}

		// Отправляем результат
		// Если текст слишком длинный, разбиваем на части
		thoughtText := string(thoughts)
		maxLength := 4000

		if len(thoughtText) <= maxLength {
			msg = tgbotapi.NewMessage(message.Chat.ID, thoughtText)
			bot.Send(msg)
		} else {
			// Разбиваем на части
			parts := (len(thoughtText) + maxLength - 1) / maxLength
			for i := 0; i < parts; i++ {
				start := i * maxLength
				end := start + maxLength
				if end > len(thoughtText) {
					end = len(thoughtText)
				}

				partText := thoughtText[start:end]
				msg = tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Часть %d/%d:\n%s", i+1, parts, partText))
				bot.Send(msg)

				// Небольшая пауза, чтобы не превысить лимиты API
				time.Sleep(100 * time.Millisecond)
			}
		}

	case "run":
		msg := tgbotapi.NewMessage(message.Chat.ID, "Отправь мне код для выполнения в следующем сообщении.")
		bot.Send(msg)
		// TODO: Реализовать запуск кода

	case "status":
		stats := getSystemStatus()
		msg := tgbotapi.NewMessage(message.Chat.ID, stats)
		bot.Send(msg)

	case "feedback":
		// Разбираем аргументы
		args := message.CommandArguments()
		parts := strings.SplitN(args, " ", 2)

		var rating int
		var comment string

		if len(parts) > 0 {
			fmt.Sscanf(parts[0], "%d", &rating)
			if len(parts) > 1 {
				comment = parts[1]
			}
		}

		// Проверяем рейтинг
		if rating < 1 || rating > 5 {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Оценка должна быть от 1 до 5.")
			bot.Send(msg)
			return
		}

		// Сохраняем обратную связь
		metadata := map[string]interface{}{
			"user_id":   message.From.ID,
			"user_name": message.From.UserName,
		}
		id, err := collector.AddFeedback(feedback.ModelOutput, "Telegram feedback", rating, comment, metadata)

		if err != nil {
			logger.Error("Failed to save feedback: %v", err)
			msg := tgbotapi.NewMessage(message.Chat.ID, "Произошла ошибка при сохранении обратной связи.")
			bot.Send(msg)
			return
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Спасибо за обратную связь! ID: %s", id))
		bot.Send(msg)

	default:
		msg := tgbotapi.NewMessage(message.Chat.ID, "Неизвестная команда. Введите /help для справки.")
		bot.Send(msg)
	}
}

// getSystemStatus возвращает текущий статус системы
func getSystemStatus() string {
	metrics := logger.GetMetrics()

	uptime := metrics.GetUptime()
	uptimeStr := fmt.Sprintf("%d дней, %d часов, %d минут",
		int(uptime.Hours())/24,
		int(uptime.Hours())%24,
		int(uptime.Minutes())%60)

	// Получаем метрики
	metricsData, _ := metrics.ToJSON()
	var metricsMap map[string]interface{}
	json.Unmarshal(metricsData, &metricsMap)

	// Формируем статус
	status := fmt.Sprintf("SmolLM Sandbox v%s\n", VERSION)
	status += fmt.Sprintf("Время работы: %s\n", uptimeStr)
	status += fmt.Sprintf("Ошибок: %d\n", metricsMap["error_count"])
	status += fmt.Sprintf("Выполнено кода: %d\n", metricsMap["executions"])

	// Добавляем информацию о системе
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	status += fmt.Sprintf("Использование памяти: %.2f МБ\n", float64(memStats.Alloc)/1024/1024)

	return status
}
