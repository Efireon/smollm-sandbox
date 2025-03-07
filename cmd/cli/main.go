package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smollm-sandbox/internal/logging"
	"smollm-sandbox/internal/model"
	"smollm-sandbox/internal/sandbox"
	"smollm-sandbox/internal/storage"
)

const (
	VERSION = "0.1.0"
)

var (
	logger        *logging.Logger
	modelInstance *model.SmolLM
	sandboxEnv    *sandbox.Environment
	store         *storage.FileSystem
)

func main() {
	// Определение флагов
	versionFlag := flag.Bool("version", false, "Вывести версию и выйти")
	configPath := flag.String("config", "configs/config.yaml", "Путь к файлу конфигурации")
	sessionFlag := flag.String("session", "", "Имя сессии (для сохранения/загрузки)")
	interactiveFlag := flag.Bool("interactive", false, "Интерактивный режим")
	thoughtFlag := flag.Bool("thought", false, "Режим размышления (без ввода пользователя)")
	thoughtTimeFlag := flag.Int("thought-time", 60, "Время размышления в секундах")
	inputFlag := flag.String("input", "", "Входной текст или файл")

	flag.Parse()

	// Проверка версии
	if *versionFlag {
		fmt.Printf("SmolLM Sandbox v%s\n", VERSION)
		os.Exit(0)
	}

	// Инициализация логирования
	logger = logging.NewLogger()
	logger.Info("Starting SmolLM Sandbox")

	// Загрузка конфигурации
	logger.Info("Loading configuration from %s", *configPath)
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

	// Загрузка сессии, если указана
	if *sessionFlag != "" {
		logger.Info("Loading session: %s", *sessionFlag)
		// TODO: Реализовать загрузку сессии
	}

	// Основная логика
	if *interactiveFlag {
		runInteractiveMode()
	} else if *thoughtFlag {
		runThoughtMode(*thoughtTimeFlag)
	} else if *inputFlag != "" {
		processInput(*inputFlag)
	} else {
		printUsage()
	}

	logger.Info("SmolLM Sandbox finished")
}

func runInteractiveMode() {
	fmt.Println("Интерактивный режим SmolLM. Введите 'exit' для выхода.")
	for {
		fmt.Print("> ")
		var input string
		fmt.Scanln(&input)

		input = strings.TrimSpace(input)
		if input == "exit" {
			break
		}

		if input == "" {
			continue
		}

		// Обработка ввода
		response := modelInstance.Process(input)
		fmt.Println(response)

		// Проверка на наличие команд
		if strings.HasPrefix(input, "/") {
			handleCommand(input)
		}
	}
}

func runThoughtMode(seconds int) {
	fmt.Printf("Запуск режима размышления на %d секунд\n", seconds)

	// Создание файла для записи размышлений
	timestamp := time.Now().Format("20060102_150405")
	thoughtFile := filepath.Join(getHomeDir(), "thoughts", fmt.Sprintf("thought_%s.txt", timestamp))

	// Убедиться, что директория существует
	os.MkdirAll(filepath.Dir(thoughtFile), 0755)

	// Запускаем размышление
	modelInstance.Think(seconds, thoughtFile)

	fmt.Printf("Размышления записаны в файл: %s\n", thoughtFile)
}

func processInput(input string) {
	// Проверяем, является ли ввод файлом
	if _, err := os.Stat(input); err == nil {
		// Читаем файл
		content, err := os.ReadFile(input)
		if err != nil {
			logger.Error("Не удалось прочитать файл: %v", err)
			return
		}
		input = string(content)
	}

	response := modelInstance.Process(input)
	fmt.Println(response)
}

func handleCommand(cmd string) {
	parts := strings.Split(cmd[1:], " ")
	command := parts[0]
	args := parts[1:]

	switch command {
	case "save":
		if len(args) == 0 {
			fmt.Println("Необходимо указать имя сессии: /save session_name")
			return
		}
		// TODO: Реализовать сохранение сессии
		fmt.Printf("Сессия сохранена как: %s\n", args[0])

	case "run":
		if len(args) == 0 {
			fmt.Println("Необходимо указать исполняемый файл: /run filename")
			return
		}
		// Запуск кода в песочнице
		output, err := sandboxEnv.Execute(args[0])
		if err != nil {
			fmt.Printf("Ошибка выполнения: %v\n", err)
		} else {
			fmt.Println(output)
		}

	case "help":
		fmt.Println("Доступные команды:")
		fmt.Println("  /save [session_name] - Сохранить текущую сессию")
		fmt.Println("  /run [filename] - Запустить файл в песочнице")
		fmt.Println("  /help - Показать эту справку")

	default:
		fmt.Printf("Неизвестная команда: %s\n", command)
		fmt.Println("Введите /help для списка команд")
	}
}

func getHomeDir() string {
	// В продакшне здесь будет домашняя директория учетной записи нейросети
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Error("Не удалось определить домашнюю директорию: %v", err)
		return "/tmp/smollm-sandbox"
	}
	return filepath.Join(homeDir, ".smollm-sandbox")
}

func printUsage() {
	fmt.Println("SmolLM Sandbox - песочница для нейросети SmolLM2")
	fmt.Println("\nИспользование:")
	flag.PrintDefaults()
	fmt.Println("\nПримеры:")
	fmt.Println("  smollm-cli --interactive                # Запуск в интерактивном режиме")
	fmt.Println("  smollm-cli --thought --thought-time=300 # Запуск режима размышления на 5 минут")
	fmt.Println("  smollm-cli --input=\"Напиши простой скрипт на Python\" # Обработка текста")
	fmt.Println("  smollm-cli --input=input.txt            # Обработка файла")
}
