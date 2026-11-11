# UFAS Docker Deployment

This project is ready to run as Docker services on Ubuntu with a Cloudflare Tunnel in front of the attendance API.

## 1. Prerequisites

- Ubuntu machine with Docker Engine and Docker Compose installed
- A Cloudflare account with a domain that you control
- A Cloudflare Tunnel configured for a hostname such as `attendance.example.com`

## 2. Prepare the environment

Copy the example env file and fill in values:

```bash
cp .env.example .env
cp app/.env.example app/.env
cp cloudflared/config.yaml.example cloudflared/config.yaml
```

Then edit:

- `.env` to set `CF_TUNNEL_ID` and `CF_HOSTNAME`
- `app/.env` to point the mobile app at the public HTTPS host

Example:

```env
CF_TUNNEL_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
CF_HOSTNAME=attendance.example.com
```

```env
EXPO_PUBLIC_API_BASE_URL=https://attendance.example.com
EXPO_PUBLIC_API_FALLBACK_BASE_URLS=http://10.0.2.2:8080,http://127.0.0.1:8080
EXPO_PUBLIC_ALLOW_HTTP=0
```

## 3. Create the Cloudflare Tunnel

On the Ubuntu host:

```bash
docker run --rm cloudflare/cloudflared:latest tunnel login
cloudflared tunnel create ufas
```

This creates a tunnel and writes a credentials file on your machine. Copy the generated file into the repo:

```bash
cp ~/.cloudflared/<TUNNEL_ID>.json ./cloudflared/creds.json
```

Then add a public hostname in Cloudflare:

```bash
cloudflared tunnel route dns ufas attendance.example.com
```

## 4. Build and start the containers

```bash
docker compose up --build -d
```

Check status:

```bash
docker compose ps
docker compose logs -f
```

## 5. Verify the services

The public attendance API is served through Cloudflare Tunnel and should be reachable at:

```bash
curl -i https://attendance.example.com/
```

The server should answer even though the root path is not a real API endpoint. For example:

```bash
curl -i -X POST https://attendance.example.com/register \
  -H 'Content-Type: application/json' \
  --data '{"code":"x","public_key":"x"}'
```

That request should return a JSON error response instead of a network error.

## 6. Admin access (Local Admin UI -> Cloudflare Tunnel)

Both `attendance-api` (port `8080`) and `cloudflared` forward `/admin/api/*` requests to `admin-server:9090` with CORS support enabled, so your local Admin UI can talk to the remote server over the exact same Cloudflare Tunnel URL used by the mobile app:

1. **Automatic via `app/.env` or `.env`**:
   - If `app/.env` has `EXPO_PUBLIC_API_BASE_URL=https://your-tunnel.trycloudflare.com` (or your custom Cloudflare hostname), running the Admin UI locally automatically proxies `/admin/api` to that Cloudflare URL:
     ```bash
     cd admin/server/webui
     npm run dev
     ```
   - Or explicitly set `VITE_API_BASE_URL=https://your-tunnel.trycloudflare.com` in `admin/server/webui/.env`.
2. **Directly inside the Admin UI browser screen**:
   - On the connection screen (or via the top-right account menu -> **Server URL**), paste your Cloudflare Tunnel URL (e.g. `https://your-tunnel.trycloudflare.com`) and click **Connect**.
3. **Host-local / SSH tunnel access**:
   - `docker-compose.yml` also binds `127.0.0.1:9090:9090`, so `http://127.0.0.1:9090/` works directly on the host or over `ssh -L 9090:127.0.0.1:9090 ubuntu@your-host`.

## 7. Persistence and upgrades

Data is stored in the Docker named volume `ufas-data` and persists across container restarts and Docker rebuilds.

To rebuild after changing code:

```bash
docker compose down
docker compose up --build -d
```

To stop everything:

```bash
docker compose down
```

## 8. Security notes

- Do not expose 8080 or 9090 directly to the internet.
- Keep `cloudflared/creds.json` outside the repo or protect it with proper file permissions.
- The mobile app must use the public HTTPS hostname, not a local Wi‑Fi IP.
- Keep admin auth and Cloudflare credentials outside the image and outside version control.

## 9. Local development continues to work

The local development setup in `start.sh` and the existing `app/.env` values remain untouched for same-network demos. The Docker deployment is an additional production path rather than a replacement for local development.
