# PROJECT_GUIDE — ipv6test

## Опис
Легковагий інструмент тестування IPv6-з'єднання. Визначає протокол (IPv4/IPv6), показує IP-адресу клієнта, та надає geo-інформацію через CLI API.

## Архітектура

```
┌─────────┐     ┌───────────┐     ┌──────────────┐
│ Браузер  │────▶│   Caddy   │────▶│  site/       │
│ / curl   │     │ (TLS,     │     │  index.html  │
└─────────┘     │  proxy)   │     └──────────────┘
                │           │     ┌──────────────┐     ┌─────────────┐
                │           │────▶│  Go API      │────▶│ ip-api.com  │
                └───────────┘     │  :8080       │     │ (GeoIP)     │
                                  └──────────────┘     └─────────────┘
```

## Компоненти

### Caddy (`Caddyfile`)
- `ipv6.0ms.app` — статичний сайт з тестом
- `0ms.app` (HTTP/HTTPS) — CLI API, браузери редиректять на ipv6.0ms.app
- `ipv4.ipv6.0ms.app` / `ipv6.ipv6.0ms.app` — субдомени для active checks

### Go API (`app/main.go`)
Ендпоінти:
| Шлях | Формат | Призначення |
|------|--------|-------------|
| `/` | text | IP клієнта (IPv6 пріоритет) |
| `/ip` | JSON | Деталі: IPv4, IPv6, headers, UA |
| `/json` | JSON | Geo-дані: city, region, country, loc, org, timezone |

GeoIP кеш: in-memory, TTL 5 хвилин. Джерело: ip-api.com (безкоштовний, HTTP).

### Docker (`docker-compose.yml`)
- `web` — Caddy (host network, TLS)
- `app` — Go API (build з `app/Dockerfile`, multi-stage)

## Запуск
```bash
docker compose up -d
```

## Використання CLI
```bash
# Текстова IP-відповідь
curl https://0ms.app

# JSON з geo-інформацією
curl https://0ms.app/json
```

## DNS вимоги
- `ipv6.0ms.app` → A + AAAA
- `ipv4.ipv6.0ms.app` → тільки A
- `ipv6.ipv6.0ms.app` → тільки AAAA
- `0ms.app` → A + AAAA
