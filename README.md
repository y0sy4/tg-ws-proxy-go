# TG WS Proxy Go

[![Go Version](https://img.shields.io/github/go-mod/go-version/y0sy4/tg-ws-proxy-go?label=Go)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/y0sy4/tg-ws-proxy-go)](https://github.com/y0sy4/tg-ws-proxy-go/releases)

> **Go-переосмысление** [Flowseal/tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy)

**Локальный SOCKS5-прокси для Telegram Desktop на Go**

Ускоряет работу Telegram через WebSocket-соединения напрямую к серверам Telegram.

## Почему Go версия лучше

| Параметр | Python | Go |
|----------|--------|-----|
| Размер | ~50 MB | **~8 MB** |
| Зависимости | pip (много) | **stdlib** |
| Время запуска | ~500 ms | **~50 ms** |
| Потребление памяти | ~50 MB | **~10 MB** |

## Быстрый старт

### Установка

```bash
# Скачать готовый бинарник из Releases
# Или собрать из исходников
go build -o TgWsProxy.exe ./cmd/proxy
```

### Запуск

```bash
# Windows (автоматически откроет настройку прокси в Telegram)
start run.bat

# Linux/macOS (автоматически откроет настройку прокси в Telegram)
./TgWsProxy

# С опциями
./TgWsProxy --port 9050 --dc-ip 2:149.154.167.220
```

## Настройка Telegram Desktop

### Автоматическая настройка

При первом запуске прокси автоматически предложит настроить Telegram (Windows).

Или откройте ссылку в браузере:
```
tg://socks?server=127.0.0.1&port=1080
```

### Ручная настройка

1. **Настройки** → **Продвинутые** → **Тип подключения** → **Прокси**
2. Добавить прокси:
   - **Тип:** SOCKS5
   - **Сервер:** `127.0.0.1`
   - **Порт:** `1080`
   - **Логин/Пароль:** пусто (или ваши данные если используете `--auth`)

Или откройте ссылку: `tg://socks?server=127.0.0.1&port=1080`

## Командная строка

```bash
./TgWsProxy [опции]

Опции:
  --port int        Порт SOCKS5 (default 1080)
  --host string     Хост SOCKS5 (default "127.0.0.1")
  --dc-ip string    DC:IP через запятую (default "2:149.154.167.220,4:149.154.167.220")
  --auth string     SOCKS5 аутентификация (username:password)
  --auto-config     Авто-настройка Telegram Desktop при запуске
  -v                Подробное логирование
  --log-file string Путь к файлу логов
  --log-max-mb float Макс. размер логов в МБ (default 5)
  --buf-kb int      Размер буфера в КБ (default 256)
  --pool-size int   Размер WS пула (default 4)
  --version         Показать версию
```

### Примеры

```bash
# Без аутентификации
./TgWsProxy -v

# С аутентификацией (защита от несанкционированного доступа)
./TgWsProxy --auth "myuser:mypassword"

# Настройка DC
./TgWsProxy --dc-ip "2:149.154.167.220,4:149.154.167.220"
```

## Структура проекта

```
tg-ws-proxy/
├── cmd/
│   └── proxy/          # CLI приложение
├── internal/
│   ├── proxy/          # Ядро прокси
│   ├── socks5/         # SOCKS5 сервер
│   ├── websocket/      # WebSocket клиент
│   ├── mtproto/        # MTProto парсинг
│   └── config/         # Конфигурация
├── go.mod
├── Makefile
└── README.md
```

## Сборка

```bash
# Все платформы
make all

# Конкретная платформа
make windows    # Windows (.exe)
make linux      # Linux (amd64)
make darwin     # macOS Intel + Apple Silicon
make android    # Android (.aar библиотека)
```

### Поддерживаемые платформы

| Платформа | Архитектуры | Статус |
|-----------|-------------|--------|
| Windows | x86_64 | ✅ Готово |
| Linux | x86_64 | ✅ Готово |
| macOS | Intel + Apple Silicon | ✅ Готово |
| Android | arm64, arm, x86_64 | 📝 См. [android/README.md](android/README.md) |
| iOS | arm64 | 🚧 В планах |

**macOS Catalina (10.15)** — поддерживается! Используйте `TgWsProxy_macos_amd64`.

## Конфигурация

Файл конфигурации:

- **Windows:** `%APPDATA%/TgWsProxy/config.json`
- **Linux:** `~/.config/TgWsProxy/config.json`
- **macOS:** `~/Library/Application Support/TgWsProxy/config.json`

```json
{
  "port": 1080,
  "host": "127.0.0.1",
  "dc_ip": [
    "1:149.154.175.50",
    "2:149.154.167.220",
    "3:149.154.175.100",
    "4:149.154.167.220",
    "5:91.108.56.100"
  ],
  "verbose": false,
  "log_max_mb": 5,
  "buf_kb": 256,
  "pool_size": 4
}
```

## Особенности

- ✅ **WebSocket pooling** — пул соединений для уменьшения задержек
- ✅ **TCP fallback** — автоматическое переключение при недоступности WS
- ✅ **MTProto парсинг** — извлечение DC ID из init-пакета
- ✅ **SOCKS5** — полная поддержка RFC 1928
- ✅ **Логирование** — с ротацией файлов
- ✅ **Zero-copy** — оптимизированные операции с памятью

## 📱 Планы развития

- [ ] **Android APK** — нативное приложение с фоновой службой
- [ ] **iOS App** — Swift обёртка вокруг Go ядра
- [ ] **GUI для desktop** — системный трей для Windows/macOS/Linux

## Производительность

| Метрика | Значение |
|---------|----------|
| Размер бинарника | ~8 MB |
| Потребление памяти | ~10 MB |
| Время запуска | <100 ms |
| Задержка (pool hit) | <1 ms |

## Требования

- **Go 1.21+** для сборки
- **Windows 7+** / **macOS 10.15+** / **Linux x86_64**
- **Telegram Desktop** для использования

## Известные ограничения

1. **IPv6** — поддерживается через IPv4-mapped адреса (::ffff:x.x.x.x) и NAT64
2. **DC3 WebSocket** — может быть недоступен в некоторых регионах

## Лицензия

MIT License

## Ссылки

- [Оригинальный проект на Python](https://github.com/Flowseal/tg-ws-proxy)
- [Документация Go](https://go.dev/)
