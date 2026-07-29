# Grafana Cloud deployment (VPS)

Run the exporter on a VPS and ship its metrics to your Grafana Cloud account.
This stack is the exporter plus a [Grafana Alloy](https://grafana.com/docs/alloy/)
collector that scrapes it and `remote_write`s to Grafana Cloud. There is **no
local Prometheus or Grafana** — storage and dashboards live in your Cloud stack.

## Setup

1. Get your Grafana Cloud Prometheus credentials: in Grafana Cloud open your
   stack → **Prometheus** → **Send Metrics**. That page shows the remote_write
   **URL** and **username** (a numeric instance ID). For the password, create an
   **access policy token** with the `metrics:write` scope.

2. Fill in the environment file:

   ```sh
   cp .env.example .env
   # edit .env: GC_PROM_URL, GC_PROM_USER, GC_PROM_TOKEN
   ```

   `.env` is gitignored — credentials never enter the repo. Alloy reads them via
   `sys.env(...)` in `config.alloy`.

3. Bring it up:

   ```sh
   docker compose up -d --build
   ```

Metrics start flowing to Grafana Cloud within a scrape interval (15s). Import
`../grafana/dashboards/atproto.json` into your Cloud Grafana to visualize them;
pick your Cloud Prometheus data source when prompted.

## Operating

- **Alloy UI** (remote_write health, scrape targets) is bound to localhost only:
  `ssh -L 12345:localhost:12345 <vps>` then open http://localhost:12345.
- **Logs:** `docker compose logs -f alloy` — a bad URL/token surfaces here as
  remote_write 401/4xx errors.
- **Cursor state** persists in the `exporter-data` volume, so a restart resumes
  the Jetstream/PLC streams instead of replaying from scratch.

The exporter itself publishes no ports on the host; only Alloy reaches it over
the compose network. For local development with a bundled Prometheus + Grafana,
use `../docker-compose.yml` instead.
