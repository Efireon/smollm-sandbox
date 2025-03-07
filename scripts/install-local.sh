#!/bin/bash

# Скрипт для установки SmolLM Sandbox из локального репозитория
# Запускать с правами root или через sudo

set -e

# Константы
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/smollm-sandbox"
MODEL_DIR="/opt/smollm-models"
SOURCE_DIR=$(pwd) # Текущая директория с исходным кодом

# Проверка прав root
if [ "$(id -u)" -ne 0 ]; then
    echo "Этот скрипт должен быть запущен с правами root или через sudo"
    exit 1
fi

# Проверка наличия необходимых инструментов
for cmd in go python3 pip; do
    if ! command -v $cmd &> /dev/null; then
        echo "Ошибка: команда $cmd не найдена"
        echo "Пожалуйста, установите необходимые зависимости"
        exit 1
    fi
done

# Создание временной директории для сборки
TMP_DIR=$(mktemp -d)
echo "Создание временной директории $TMP_DIR"

# Очистка при выходе
cleanup() {
    echo "Очистка временных файлов..."
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Копирование исходного кода
echo "Копирование исходного кода..."
cp -r "$SOURCE_DIR"/* "$TMP_DIR"
cd "$TMP_DIR"

# Сборка приложения
echo "Сборка приложения..."
go build -o smollm-cli ./cmd/cli
go build -o smollm-telegram ./cmd/telegram

# Создание директорий
echo "Создание директорий..."
mkdir -p "$CONFIG_DIR" "$MODEL_DIR"

# Копирование исполняемых файлов
echo "Установка исполняемых файлов..."
cp smollm-cli "$INSTALL_DIR/"
cp smollm-telegram "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/smollm-cli" "$INSTALL_DIR/smollm-telegram"

# Копирование конфигурационных файлов
echo "Установка конфигурационных файлов..."
cp configs/config.yaml "$CONFIG_DIR/"
cp configs/sandbox_config.yaml "$CONFIG_DIR/"

# Загрузка модели
echo "Скачивание модели SmolLM2..."
mkdir -p "$MODEL_DIR/SmolLM2-135M-Instruct"

# Запрос на скачивание модели
read -p "Хотите скачать модель SmolLM2 (размер ~135MB)? [y/N] " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    # Создаем виртуальное окружение Python
    echo "Создание виртуального окружения Python..."
    python -m venv /tmp/smollm_venv
    source /tmp/smollm_venv/bin/activate
    
    # Устанавливаем необходимые Python-зависимости
    echo "Установка Python-зависимостей..."
    pip install huggingface_hub
    
    # Используем Python для скачивания модели через Hugging Face
    python -c "from huggingface_hub import snapshot_download; snapshot_download(repo_id='HuggingFaceTB/SmolLM2-135M-Instruct', local_dir='$MODEL_DIR/SmolLM2-135M-Instruct', local_dir_use_symlinks=False)"
    
    # Деактивируем виртуальное окружение
    deactivate
    rm -rf /tmp/smollm_venv
    echo "Модель установлена в $MODEL_DIR/SmolLM2-135M-Instruct"
else
    echo "Пропуск скачивания модели"
fi

# Создание пользователя для нейросети
echo "Создание пользователя для нейросети..."
bash scripts/setup_user.sh

# Настройка автозапуска
echo "Настройка автозапуска..."
systemctl enable smollm-sandbox.service

echo "Установка завершена успешно!"
echo "Для запуска CLI интерфейса: smollm-cli --interactive"
echo "Для запуска сервиса: systemctl start smollm-sandbox"