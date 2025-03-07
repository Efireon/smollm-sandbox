package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	inputScanner  *bufio.Scanner
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
	logger.Info("Starting SmolLM Sandbox v%s", VERSION)

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

	// Инициализация сканера ввода
	inputScanner = bufio.NewScanner(os.Stdin)

	// Настройка обработки сигналов для корректного завершения
	setupSignalHandler()

	// Загрузка сессии, если указана
	if *sessionFlag != "" {
		logger.Info("Loading session: %s", *sessionFlag)
		if err := modelInstance.LoadSession(*sessionFlag); err != nil {
			logger.Error("Failed to load session: %v", err)
			fmt.Printf("Ошибка загрузки сессии: %v\n", err)
		} else {
			fmt.Printf("Сессия '%s' успешно загружена\n", *sessionFlag)
		}
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

	// Освобождаем ресурсы
	modelInstance.Close()

	logger.Info("SmolLM Sandbox finished")
}

func runInteractiveMode() {
	fmt.Println("SmolLM Sandbox v" + VERSION + " интерактивный режим")
	fmt.Println("Введите текст для общения с нейросетью или команду (/help для списка команд)")
	fmt.Println("Для выхода введите 'exit'")

	for {
		fmt.Print("\n> ")
		var input string

		if inputScanner.Scan() {
			input = inputScanner.Text()
		} else {
			break // Обрабатываем ошибку или EOF
		}

		input = strings.TrimSpace(input)
		if input == "exit" {
			break
		}

		if input == "" {
			continue
		}

		// Проверка на наличие команд
		if strings.HasPrefix(input, "/") {
			handleCommand(input)
			continue
		}

		// Обработка ввода
		fmt.Println("\nОбработка запроса... Пожалуйста, подождите.")
		response := modelInstance.Process(input)
		fmt.Printf("\n%s\n", response)
	}
}

func runThoughtMode(seconds int) {
	fmt.Printf("Запуск режима размышления на %d секунд\n", seconds)

	// Создание файла для записи размышлений
	timestamp := time.Now().Format("20060102_150405")
	thoughtFile := filepath.Join(getHomeDir(), "thoughts", fmt.Sprintf("thought_%s.txt", timestamp))

	// Убедиться, что директория существует
	os.MkdirAll(filepath.Dir(thoughtFile), 0755)

	fmt.Println("Запуск режима размышления. Это займет некоторое время...")
	fmt.Printf("Размышления будут сохранены в: %s\n", thoughtFile)

	// Запускаем размышление
	modelInstance.Think(seconds, thoughtFile)

	fmt.Println("Режим размышления завершен!")
	fmt.Printf("Размышления записаны в файл: %s\n", thoughtFile)
}

func processInput(input string) {
	// Проверяем, является ли ввод файлом
	if _, err := os.Stat(input); err == nil {
		// Читаем файл
		content, err := os.ReadFile(input)
		if err != nil {
			logger.Error("Не удалось прочитать файл: %v", err)
			fmt.Printf("Ошибка: не удалось прочитать файл: %v\n", err)
			return
		}
		input = string(content)
		fmt.Println("Обработка содержимого файла...")
	} else {
		fmt.Println("Обработка текста...")
	}

	response := modelInstance.Process(input)
	fmt.Printf("\n%s\n", response)
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

		// Сохраняем сессию
		if err := modelInstance.SaveSession(args[0]); err != nil {
			logger.Error("Failed to save session: %v", err)
			fmt.Printf("Ошибка сохранения сессии: %v\n", err)
		} else {
			fmt.Printf("Сессия сохранена как: %s\n", args[0])
		}

	case "load":
		if len(args) == 0 {
			fmt.Println("Необходимо указать имя сессии: /load session_name")
			return
		}

		// Загружаем сессию
		if err := modelInstance.LoadSession(args[0]); err != nil {
			logger.Error("Failed to load session: %v", err)
			fmt.Printf("Ошибка загрузки сессии: %v\n", err)
		} else {
			fmt.Printf("Сессия '%s' успешно загружена\n", args[0])
		}

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

	case "code":
		if len(args) < 2 {
			fmt.Println("Необходимо указать язык и код: /code [python|js|go] \"код для выполнения\"")
			return
		}

		language := args[0]
		code := strings.Join(args[1:], " ")

		// Выполняем код в песочнице
		output, err := sandboxEnv.ExecuteCode(code, language)
		if err != nil {
			fmt.Printf("Ошибка выполнения кода: %v\n", err)
		} else {
			fmt.Println(output)
		}

	case "help":
		fmt.Println("Доступные команды:")
		fmt.Println("  /save [session_name] - Сохранить текущую сессию")
		fmt.Println("  /load [session_name] - Загрузить сохраненную сессию")
		fmt.Println("  /run [filename] - Запустить файл в песочнице")
		fmt.Println("  /code [language] [code] - Выполнить строку кода")
		fmt.Println("  /help - Показать эту справку")
		fmt.Println("  exit - Выйти из программы")

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

func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\nПолучен сигнал завершения. Освобождение ресурсов...")

		// Закрываем соединения и освобождаем ресурсы
		if modelInstance != nil {
			modelInstance.Close()
		}

		logger.Info("Graceful shutdown completed")
		os.Exit(0)
	}()
}
