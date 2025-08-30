# IPv6 Connectivity Test Page

This project provides a minimal **IPv6 connectivity test site** using [Caddy](https://caddyserver.com/) inside Docker.  
It serves a simple HTML page that:

- Detects whether your connection uses **IPv4** or **IPv6**.
- Runs client-side checks by loading tiny images from dedicated IPv4- and IPv6-only subdomains.
- Automatically provisions HTTPS certificates via Let's Encrypt.
- (Optionally) Uses Matomo for anonymous analytics hosted at `ipv60msapp.matomo.cloud`.

---

## Quick Start

1. **Clone the repository**:
   ```bash
   git clone https://github.com/bgpntx/ipv6test.git
   cd ipv6test
---

## Requirements

- A server with Docker + Docker Compose installed
- Domain names with DNS records:
  - `ipv6.0ms.app` → **A** + **AAAA**
  - `ipv4.ipv6.0ms.app` → **A only** (no AAAA)
  - `ipv6.ipv6.0ms.app` → **AAAA only** (no A)
- Open inbound ports `80/tcp` and `443/tcp` to the server

---

## Files

- `docker-compose.yml` – runs the Caddy container
- `Caddyfile` – defines domains and site config
- `site/index.html` – the test page with Spinnaker font, IPv4/IPv6 checks, and Matomo snippet
- `site/ping.png` – tiny 1×1 transparent PNG used for connectivity tests

---

## Usage

1. Clone this repo (or copy the files to a directory on your server).
2. Adjust the **email address** in `docker-compose.yml` and `Caddyfile` for Let’s Encrypt.
3. Start the stack:

   ```bash
   docker compose up -d
