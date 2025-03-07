#!/bin/bash

# Скрипт для создания системного пользователя для нейросети SmolLM2
# Запускать с правами root или через sudo

set -e

# Константы
USERNAME="smollm"
HOME_DIR="/home/$USERNAME"
WORK_DIR="$HOME_DIR/workspace"
CODE_DIR="$WORK_DIR/code"
DATA_DIR="$WORK_DIR/data"
THOUGHTS_DIR="$WORK_DIR/thoughts"
SESSION_DIR="$WORK_DIR/sessions"

# Проверка прав root
if [ "$(id -u)" -ne 0 ]; then
    echo "Этот скрипт должен быть запущен с правами root или через sudo"
    exit 1
fi

# Создание пользователя, если он не существует
if id "$USERNAME" &>/dev/null; then
    echo "Пользователь $USERNAME уже существует"
else
    echo "Создание пользователя $USERNAME..."
    useradd -m -s /bin/bash "$USERNAME"
    # Генерируем случайный пароль (не будет использоваться для входа)
    PASSWD=$(openssl rand -base64 12)
    echo "$USERNAME:$PASSWD" | chpasswd
    echo "Пользователь $USERNAME создан"
fi

# Создание рабочих директорий
echo "Создание рабочих директорий..."
mkdir -p "$CODE_DIR" "$DATA_DIR" "$THOUGHTS_DIR" "$SESSION_DIR"

# Устанавливаем права
chown -R "$USERNAME:$USERNAME" "$HOME_DIR"
chmod -R 755 "$HOME_DIR"

# Установка ограничений на ресурсы
echo "Установка ограничений на ресурсы..."

# Создаем файл для конфигурации limits
cat > /etc/security/limits.d/smollm.conf << EOF
# Ограничения для пользователя netsim
$USERNAME soft nproc 50
$USERNAME hard nproc 100
$USERNAME soft nofile 1024
$USERNAME hard nofile 4096
$USERNAME soft cpu 60
$USERNAME hard cpu 90
$USERNAME soft as 1048576
$USERNAME hard as 2097152
EOF

# Создаем файл tmpfiles.d для ограничения /tmp
cat > /etc/tmpfiles.d/smollm.conf << EOF
# Создать директорию в /tmp для пользователя $USERNAME с ограничением размера
d /tmp/$USERNAME 1770 $USERNAME $USERNAME - -
EOF

# Применяем конфигурацию tmpfiles
systemd-tmpfiles --create

# Создаем systemd юнит для автоматического запуска sandbox
echo "Создание systemd сервиса..."
cat > /etc/systemd/system/smollm-sandbox.service << EOF
[Unit]
Description=SmolLM2 Sandbox Service
After=network.target

[Service]
Type=simple
User=$USERNAME
WorkingDirectory=$WORK_DIR
ExecStart=/usr/local/bin/smollm-cli --thought --thought-time=3600
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=smollm-sandbox

# Ограничения ресурсов
CPUQuota=50%
MemoryLimit=1G

[Install]
WantedBy=multi-user.target
EOF

# Перезагружаем конфигурацию systemd
systemctl daemon-reload

# Устанавливаем необходимые компиляторы и интерпретаторы
echo "Установка необходимых пакетов..."
if [ -f /etc/debian_version ]; then
    # Debian/Ubuntu
    apt-get update
    apt-get install -y python3 python3-pip nodejs npm gcc g++ golang
elif [ -f /etc/arch-release ]; then
    # Arch Linux
    pacman -Sy --noconfirm python python-pip nodejs npm gcc go
elif [ -f /etc/fedora-release ]; then
    # Fedora
    dnf install -y python3 python3-pip nodejs gcc gcc-c++ golang
else
    echo "Неподдерживаемый дистрибутив. Установите пакеты вручную."
fi

echo "Настройка завершена."
echo "Для запуска сервиса выполните: systemctl enable --now smollm-sandbox"
echo "Для проверки статуса: systemctl status smollm-sandbox"