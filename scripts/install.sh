#!/bin/bash

# Скрипт для установки SmolLM Sandbox
# Запускать с правами root или через sudo

set -e

# Константы
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/smollm-sandbox"
MODEL_DIR="/opt/smollm-models"
REPO_URL="https://github.com/yourusername/smollm-sandbox"
BRANCH="main"

# Проверка прав root
if [ "$(id -u)" -ne 0 ]; then
    echo "Этот скрипт должен быть запущен с правами root или через sudo"
    exit 1
fi

# Проверка наличия необходимых инструментов
for cmd in git go python3 pip; do
    if ! command -v $cmd &> /dev/null; then
        echo "Ошибка: команда $cmd не найдена"
        echo "Пожалуйста, установите необходимые зависимости"
        exit 1
    fi
done

# Создание временной директории
TMP_DIR=$(mktemp -d)
echo "Создание временной директории $TMP_DIR"

# Очистка при выходе
cleanup() {
    echo "Очистка временных файлов..."
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Клонирование репозитория
echo "Клонирование репозитория $REPO_URL..."
git clone --branch "$BRANCH" "$REPO_URL" "$TMP_DIR"
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

# Используем Python для скачивания модели через Hugging Face
python3 -c "
from huggingface_hub import snapshot_download
snapshot_download(
    repo_id='HuggingFaceTB/SmolLM2-135M-Instruct',
    local_dir='$MODEL_DIR/SmolLM2-135M-Instruct',
    local_dir_use_symlinks=False
)
"

echo "Модель установлена в $MODEL_DIR/SmolLM2-135M-Instruct"

# Создание пользователя для нейросети
echo "Создание пользователя для нейросети..."
bash scripts/setup_user.sh

# Настройка автозапуска
echo "Настройка автозапуска..."
systemctl enable smollm-sandbox.service

echo "Установка завершена успешно!"
echo "Для запуска CLI интерфейса: smollm-cli --interactive"
echo "Для запуска сервиса: systemctl start smollm-sandbox"