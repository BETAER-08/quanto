# Deploying quanto with Podman and Quadlet

quanto ships as a single static binary in a distroless container image. A deployment consists of five Quadlet files in `deploy/quadlet/`:

| File | Purpose |
|---|---|
| `quanto.network` | Container network `quanto` shared by all containers |
| `quanto-db.volume` | Volume `quanto-db` for PostgreSQL data |
| `quanto-db.container` | PostgreSQL 16 (`docker.io/library/postgres:16`) |
| `quanto-web.container` | `quanto serve --role web`, published on `127.0.0.1:8080` |
| `quanto-worker.container` | `quanto serve --role worker` |

The web and worker units require and start after `quanto-db.service`. Both roles apply pending database migrations at startup under a PostgreSQL advisory lock, so starting them at the same time is safe. The units restart on failure, which also covers the window in which PostgreSQL is still initializing.

The units were checked with the Quadlet generator of Podman 4.9 (`/usr/libexec/podman/quadlet -dryrun -user`).

## 1. Build the image

Run as the user that will run the services, so the image is in that user's Podman storage:

```sh
make image
```

This runs `podman build -f deploy/Containerfile --ignorefile deploy/.containerignore`, which leaves `.git`, `bin` and `testdata/corpus` out of the build context, and tags the result as `localhost/quanto:<version>` and `localhost/quanto:latest`. The Quadlet units use `localhost/quanto:latest`. Check the build:

```sh
podman run --rm localhost/quanto:latest version
```

## 2. Create the Podman secrets

The web and worker containers read the App private key and the webhook secret from files that Podman mounts at `/run/secrets/<name>`, referenced by `QUANTO_PRIVATE_KEY_FILE` and `QUANTO_WEBHOOK_SECRET_FILE`. The database URL and the App ID are injected as the environment variables `QUANTO_DATABASE_URL` and `QUANTO_APP_ID` from secrets of the same kind, so no credential is written into a unit file.

```sh
DB_PASSWORD="$(openssl rand -hex 24)"

printf '%s' "$DB_PASSWORD" | podman secret create quanto-db-password -
printf 'postgres://quanto:%s@quanto-db:5432/quanto?sslmode=disable' "$DB_PASSWORD" \
  | podman secret create quanto-database-url -
unset DB_PASSWORD

printf '%s' '<app id>' | podman secret create quanto-app-id -
podman secret create quanto-private-key ./<app-name>.<date>.private-key.pem
printf '%s' '<webhook secret>' | podman secret create quanto-webhook-secret -
```

Replace the values in angle brackets with the App ID, the downloaded private key file, and the webhook secret from [github-app.md](github-app.md). Trailing newlines in the key and secret files are removed when quanto reads them.

`podman secret ls` lists the five secrets: `quanto-db-password`, `quanto-database-url`, `quanto-app-id`, `quanto-private-key`, `quanto-webhook-secret`.

## 3. Install the Quadlet units

```sh
mkdir -p ~/.config/containers/systemd/
cp deploy/quadlet/* ~/.config/containers/systemd/
systemctl --user daemon-reload
systemctl --user start quanto-db.service quanto-web.service quanto-worker.service
```

Check the services:

```sh
systemctl --user status quanto-web.service quanto-worker.service
journalctl --user -u quanto-web.service -u quanto-worker.service
curl -s http://127.0.0.1:8080/healthz
curl -s http://127.0.0.1:8080/readyz
```

`/healthz` returns `ok` while the process runs. `/readyz` returns 200 when the database answers a ping and 503 otherwise. `/metrics` serves Prometheus metrics.

The units carry `WantedBy=default.target`, so they start with the user's systemd instance. To start them at boot without an interactive login, enable lingering for the user:

```sh
loginctl enable-linger "$USER"
```

## 4. Optional settings

Add `Environment=` lines to the `[Container]` section of `quanto-web.container` and `quanto-worker.container` to change these settings, then run `systemctl --user daemon-reload` and restart the services:

| Variable | Default | Meaning |
|---|---|---|
| `QUANTO_LISTEN_ADDR` | `:8080` | Address of the web role inside the container |
| `QUANTO_GITHUB_API_URL` | `https://api.github.com` | GitHub REST API base URL |
| `QUANTO_WORKER_CONCURRENCY` | `4` | Worker goroutines (1–64) |
| `QUANTO_ALLOW_PRIVATE_REPOS` | `false` | Analyze private repositories |
| `QUANTO_MAX_WORKFLOW_FILES` | `50` | Workflow files analyzed per pull request (1–200) |
| `QUANTO_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

quanto validates all settings at startup and reports every invalid value in one error.

## 5. TLS and the reverse proxy

The web container is published on `127.0.0.1:8080` only and serves plain HTTP. GitHub must reach the webhook over HTTPS, so a reverse proxy on the host has to terminate TLS for the public hostname and forward requests to `http://127.0.0.1:8080`. GitHub only needs `POST /webhook`; `/healthz`, `/readyz` and `/metrics` do not have to be exposed publicly. The request body limit of the proxy must allow webhook payloads up to 25 MiB, which is the limit quanto enforces.

The webhook URL of the GitHub App is `https://<public hostname>/webhook`.

## 6. Local development

For development, run quanto directly against a local PostgreSQL database:

```sh
make build
export QUANTO_DATABASE_URL='postgres://quanto:quanto@localhost:5432/quanto?sslmode=disable'
export QUANTO_APP_ID='<app id>'
export QUANTO_PRIVATE_KEY_FILE="$HOME/.config/quanto/private-key.pem"
export QUANTO_WEBHOOK_SECRET_FILE="$HOME/.config/quanto/webhook-secret"
./bin/quanto migrate
./bin/quanto serve --role all
```

`--role all` runs the web and worker roles in one process.

GitHub cannot deliver webhooks to `localhost`. Use a webhook forwarding channel such as [smee.io](https://smee.io): create a channel, set the channel URL as the webhook URL of a separate development App, and forward deliveries to the local server:

```sh
npx smee-client --url https://smee.io/<channel> --target http://127.0.0.1:8080/webhook
```

The forwarder keeps the original `X-Hub-Signature-256`, `X-GitHub-Event` and `X-GitHub-Delivery` headers, so signature verification works unchanged. Past deliveries can be re-sent from **Advanced → Recent Deliveries → Redeliver** on the App settings page; deliveries already processed are acknowledged as `duplicate`.
