# IPv6 Connectivity Test

[ipv6.0ms.app](https://ipv6.0ms.app) — легковагий інструмент для тестування IPv6-з'єднання, побудований на [Caddy](https://caddyserver.com/) та Docker.

## Можливості

- Визначає чи підключення через **IPv4** чи **IPv6**
- Активні клієнтські тести через IPv4-only та IPv6-only субдомени
- **CLI API** — `curl 0ms.app` повертає IP, `curl 0ms.app/json` повертає geo-дані (як ipinfo.io)
- Автоматичні HTTPS сертифікати через Let's Encrypt
- Аналітика через Matomo (анонімна, без cookies)

## Швидкий старт

```bash
git clone https://github.com/bgpntx/ipv6test.git
cd ipv6test
docker compose up -d
```

## Вимоги

- Сервер з Docker + Docker Compose
- DNS записи:
  - `ipv6.0ms.app` → **A** + **AAAA**
  - `ipv4.ipv6.0ms.app` → **тільки A** (без AAAA)
  - `ipv6.ipv6.0ms.app` → **тільки AAAA** (без A)
  - `0ms.app` → **A** + **AAAA** (для CLI API)
- Відкриті порти `80/tcp` та `443/tcp`

## Структура

```
├── Caddyfile              # Конфігурація доменів та проксі
├── docker-compose.yml     # Docker контейнери (Caddy + Go API)
├── Jenkinsfile            # CI/CD пайплайн
├── app/
│   ├── main.go            # Go API сервер
│   ├── go.mod             # Go модуль
│   └── Dockerfile         # Multi-stage build (scratch)
└── site/
    ├── index.html         # Сторінка тестування з Caddy templates
    └── ping.png           # 1×1 PNG для active checks
```

## CLI API

```bash
# Отримати свою IP-адресу
curl https://0ms.app

# Отримати geo-інформацію (формат ipinfo.io)
curl https://0ms.app/json

# Приклад відповіді /json:
# {
#   "ip": "2a01:4f8:c17:...",
#   "city": "Dublin",
#   "region": "Leinster",
#   "country": "IE",
#   "loc": "53.3331,-6.2489",
#   "org": "AS216050 Lietparkas UAB",
#   "timezone": "Europe/Dublin"
# }
```

## Ліцензія

[GPL-3.0](LICENSE)
