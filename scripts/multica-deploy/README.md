# multica-deploy

Scripts for the `multica2` deployment on the host **`admin@100.69.248.97`**
(SSH alias `multica`). Everything here assumes that specific machine — paths,
container names, daemon profiles and the docker-compose file are hard-coded
in `config.sh`. They will NOT work on `admin` (100.82.242.203, bare-process
deployment) or on a fresh self-hosted install.

These scripts run **on the multica machine itself** (typically invoked via
`ssh multica 'cd /home/admin/multica && bash scripts/multica-deploy/<script>'`).
They are NOT meant to be run from a developer laptop.

## What the machine actually has

- Git checkout at `/home/admin/multica`, remotes include `crimson`
  (`git@github.com:crimson-gao/multica.git`) and `origin`.
- Docker compose stack (`multica2-backend-1`, `multica2-frontend-1`,
  `multica2-postgres-1`) defined in `docker-compose.multica2.yml` with env
  in `.env.multica2`.
- 4 daemon profiles running as bare `nohup` processes (PPid=1), CLI binary
  at both `/usr/local/bin/multica` (root-owned, sudo to overwrite) and
  `/apsara/data1/home-admin-offload/multica/server/bin/multica` (admin-owned).
- Go toolchain at `/home/admin/go/bin/go` (1.26.1).
- pnpm via nvm at `/home/admin/.nvm/versions/node/v22.22.3/bin/pnpm`.
- Passwordless `sudo` for the `admin` user.

## Scripts

| Script | What it does |
|---|---|
| `build-backend.sh [--commit REV]` | `go build` server + migrate + cli with ldflags, drop into `.deploy-artifacts/backend/`, `docker build` an image tagged `multica2-backend:candidate-<commit>` |
| `build-frontend.sh [--commit REV]` | `pnpm build` the Next.js standalone (with `REMOTE_API_URL=http://backend:8080`), sync to `.deploy-artifacts/web/`, build image `multica2-web:candidate-<commit>` |
| `deploy-backend.sh --commit REV` | Backup current `:artifact` → `:rollback-pre-<commit>`, promote candidate, recreate container, wait for `/health` to return 200 |
| `deploy-frontend.sh --commit REV` | Same shape, for the web container, waits for `http://localhost:13000/` |
| `deploy-cli.sh` | Backup both CLI binary paths, stop 4 daemon profiles, copy in the freshly-built `multica` binary, restart each profile with its original `nohup` cmdline |
| `rollback-backend.sh` | Restore the most recent `:rollback-pre-*` tag for backend and recreate |
| `rollback-frontend.sh` | Same for frontend |
| `release.sh [--commit REV] [--skip-cli]` | End-to-end: `git pull crimson <branch>`, then run build-* + deploy-* in safe order, gated on health checks. CLI step is opt-out |
| `status.sh` | Print current image tags, container status, daemon status, last few `:rollback-pre-*` / `:candidate-*` tags so an operator can tell at a glance "what's running and what's available to roll back to" |
| `config.sh` | Sourced by every script. Defines container/image names, ports, paths, daemon profile cmdlines. Edit here, not in individual scripts |

## Daemon profiles

The cmdlines below are reproduced verbatim from `/proc/<pid>/cmdline` at the
time these scripts were written. If you add a new profile, add it to
`DAEMON_PROFILES` in `config.sh` along with its full cmdline; `deploy-cli.sh`
loops over them.

| Profile | Device name | Binary path |
|---|---|---|
| (default) | (hostname) | `/usr/local/bin/multica` |
| `multica2` | `multica2-local` | `/apsara/data1/home-admin-offload/multica/server/bin/multica` |
| `agentloop-2` | `agentloop-dev-2` | `/usr/local/bin/multica` |
| `376610` | `376610@alibaba-inc.com` | `/usr/local/bin/multica` |

## Common flags

Every script accepts:

- `--dry-run` — print what would happen, do nothing. Builds skip the actual
  `go build` / `pnpm build` / `docker build`; deploys skip the recreate.
- `-h` / `--help` — show usage.

## Where backups live

- Docker images: tagged `multica2-{backend,web}:rollback-pre-<commit>`.
  `status.sh` lists them. `docker image prune` won't touch them as long
  as they have a tag.
- Artifact directories: `.deploy-artifacts/{backend,web}.bak.<YYYYMMDDHHMMSS>/`
- CLI binaries: `*.bak.<YYYYMMDDHHMMSS>` next to the live binary
- Env file: `.env.multica2.bak.<YYYYMMDDHHMMSS>`

Backups are never pruned by these scripts. Clean them out manually when disk
fills up (`du -sh .deploy-artifacts/*.bak.*` to see sizes).

## Typical flows

**Routine release** (you pushed a commit to crimson and want it live):
```
ssh multica 'cd /home/admin/multica && bash scripts/multica-deploy/release.sh'
```

**Component-by-component** (you only changed the frontend):
```
ssh multica 'cd /home/admin/multica && bash scripts/multica-deploy/build-frontend.sh && bash scripts/multica-deploy/deploy-frontend.sh'
```

**Emergency rollback** (something just broke):
```
ssh multica 'cd /home/admin/multica && bash scripts/multica-deploy/rollback-backend.sh && bash scripts/multica-deploy/rollback-frontend.sh'
```

**Where are we?** :
```
ssh multica 'cd /home/admin/multica && bash scripts/multica-deploy/status.sh'
```
