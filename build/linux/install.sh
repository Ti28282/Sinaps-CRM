#!/usr/bin/env bash
# Ставит ярлык SINAPS CRM в меню приложений и док Ubuntu (только для текущего пользователя).
# Запуск из корня проекта после `wails build`:  bash build/linux/install.sh
set -euo pipefail
cd "$(dirname "$0")/../.."

BIN="$(pwd)/build/bin/SINAPS-CRM"
[ -x "$BIN" ] || { echo "Сначала соберите: wails build"; exit 1; }

ICON_DIR="$HOME/.local/share/icons/hicolor/512x512/apps"
APP_DIR="$HOME/.local/share/applications"
mkdir -p "$ICON_DIR" "$APP_DIR"
cp build/linux/sinaps-crm.png "$ICON_DIR/sinaps-crm.png"

cat > "$APP_DIR/sinaps-crm.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=SINAPS CRM
Comment=Клиенты из Telegram в одной воронке
Exec=$BIN
Icon=sinaps-crm
Terminal=false
Categories=Office;Network;
StartupWMClass=sinaps-crm
DESKTOP

gtk-update-icon-cache -q "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
update-desktop-database -q "$APP_DIR" 2>/dev/null || true
echo "Готово: SINAPS CRM есть в меню приложений. Закрепите в доке через ПКМ → «Добавить в избранное»."
