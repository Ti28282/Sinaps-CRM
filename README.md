# SINAPS CRM

Десктоп-CRM: чаты из папки Telegram попадают в воронку, база хранится в одном файле SQLite на компьютере.

**Стек:** Go 1.24 + Wails v2 (окно) · React 18 + Vite (статика вшита в бинарник) · SQLite через `modernc.org/sqlite` (чистый Go, без CGO) · Telegram через `gotd/td` (MTProto).

## Сборка

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
go mod tidy
wails build              # → build/bin/SINAPS-CRM.exe
wails dev                # разработка с hot reload
```

Только фронт в браузере, с демо-данными и без Go:

```bash
cd frontend && npm i && npm run dev
```

Нужны Go 1.24+ и Node 20+. На Windows 10/11 WebView2 уже стоит.

## Первый запуск

1. Получите **API ID / API Hash** на my.telegram.org → API development tools.
2. Откройте «Подключения», введите ключи, номер, код и пароль 2FA, если он включён.
3. Выберите папку (по умолчанию ищется «clients») и нажмите «Импортировать».

Данные лежат в `%AppData%\SINAPS-CRM\` (`crm.db` и `tg-session.json`). Файл сессии = доступ к аккаунту, не передавайте его никому.

## Почему нет дублей

| Таблица | Уникальный ключ | При повторном импорте |
|---|---|---|
| `chats` | `(channel, ext_id)`, где `ext_id` = `u123` / `c45` / `ch678` | обновляются имя и username; этап, сумма, заметка и теги **не трогаются** |
| `messages` | `(chat_id, msg_id)` | `ON CONFLICT DO NOTHING`, докачиваются только новые |

`channel` = `tg` или `wa`, так что WhatsApp встанет в ту же схему.

## Что умеет 0.1

- Импорт папки и последних N сообщений (по умолчанию 200) с обработкой FLOOD_WAIT.
- Живые сообщения (события Wails `message:new` и `chats:changed`), счётчик непрочитанных.
- Канбан с drag-and-drop: Новые → Написал → Ответил → Сделка → Потерян.
- Автоматическая смена этапа: ответ из CRM переводит «Новые» в «Написал», ответ клиента переводит «Написал» в «Ответил».
- Карточка клиента: сумма, следующий шаг, теги, заметка, история.
- Рассылки по шаблону (`{name}`, `{title}`, `{username}`): лимит в день (по умолчанию 20), паузы 40–90 с, остановка при `PEER_FLOOD`.

Папка разбирается так же, как в Telegram: вручную добавленные чаты + правила («Контакты», «Группы», «Боты»…) минус исключения и «Без звука/Прочитанные/Архив».

## Структура

```
main.go                     Wails: окно, embed frontend/dist
app.go                      методы для фронта + рассылки
internal/store/             SQLite: схема, upsert, тест на дубли
internal/telegram/          вход, папки, история, обновления, отправка
frontend/src/api.js         мост window.go.main.App + демо-режим (mock.js)
frontend/src/pages/         Pipeline, Chats, Broadcast, Connect
```

Тест базы: `go test ./internal/store/`

## Дальше

- WhatsApp через `go.mau.fi/whatsmeow` (QR, ярлык «clients»). Лучше на отдельном номере.
- ИИ-черновики ответов.
- Экспорт в CSV.
