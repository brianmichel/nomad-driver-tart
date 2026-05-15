job "cursor-self-hosted-worker" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel      = 0
    healthy_deadline  = "30m"
    progress_deadline = "60m"
  }

  group "workers" {
    count = 1

    constraint {
      attribute = attr.driver.tart.available_slots
      value     = "true"
    }

    task "vm" {
      driver = "tart"

      env {
        CURSOR_REPO_URL = "https://github.com/your-org/your-repo.git"
        CURSOR_REPO_REF = "main"
        CURSOR_API_KEY  = "your-cursor-api-key"
      }

      template {
        data = <<EOH
CURSOR_API_KEY={{ env "CURSOR_API_KEY" }}
CURSOR_REPO_URL={{ env "CURSOR_REPO_URL" }}
CURSOR_REPO_REF={{ env "CURSOR_REPO_REF" }}
CURSOR_WORKER_HOSTNAME=cursor-worker-{{ env "NOMAD_ALLOC_ID" }}
SSH_PASSWORD={{ with nomadVar "nomad/jobs/cursor-self-hosted-worker" }}{{ .ssh_password }}{{ end }}
EOH
        destination = "secrets/cursor.env"
      }

      template {
        data = <<EOF
#!/bin/bash
set -euo pipefail

SECRETS_FILE="/Volumes/My Shared Files/secrets/cursor.env"
if [[ ! -f "$SECRETS_FILE" ]]; then
  echo "cursor: secrets file missing at $SECRETS_FILE"
  exit 1
fi

set -a
source "$SECRETS_FILE"
set +a

if [[ -z "$${CURSOR_API_KEY:-}" ]]; then
  echo "cursor: CURSOR_API_KEY is not set"
  exit 1
fi

if [[ -z "$${CURSOR_REPO_URL:-}" ]]; then
  echo "cursor: CURSOR_REPO_URL is not set"
  exit 1
fi

if [[ -n "$${SSH_PASSWORD:-}" ]]; then
  echo "cursor: unlocking login keychain"
  security unlock-keychain -p "$SSH_PASSWORD" "$HOME/Library/Keychains/login.keychain-db" || true
  security default-keychain -s "$HOME/Library/Keychains/login.keychain-db" || true
  security set-keychain-settings "$HOME/Library/Keychains/login.keychain-db" || true
fi

if [[ -n "$${CURSOR_WORKER_HOSTNAME:-}" && -n "$${SSH_PASSWORD:-}" ]]; then
  HOSTNAME_SAFE="$(printf '%s' "$CURSOR_WORKER_HOSTNAME" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9-' '-' | sed 's/^-*//; s/-*$//' | cut -c1-63)"
  if [[ -n "$HOSTNAME_SAFE" ]]; then
    echo "cursor: setting hostname to $HOSTNAME_SAFE"
    echo "$SSH_PASSWORD" | sudo -S scutil --set HostName "$HOSTNAME_SAFE" >/dev/null 2>&1 || true
    echo "$SSH_PASSWORD" | sudo -S scutil --set LocalHostName "$HOSTNAME_SAFE" >/dev/null 2>&1 || true
    echo "$SSH_PASSWORD" | sudo -S scutil --set ComputerName "$HOSTNAME_SAFE" >/dev/null 2>&1 || true
    echo "$SSH_PASSWORD" | sudo -S hostname "$HOSTNAME_SAFE" >/dev/null 2>&1 || true
  fi
fi

export PATH="$HOME/.cursor/bin:$HOME/.local/bin:$PATH"
WORK_ROOT="$HOME/cursor-worker"
REPO_DIR="$WORK_ROOT/repo"

mkdir -p "$WORK_ROOT"

echo "cursor: startup begin $(date -u +%FT%TZ)"
echo "cursor: hostname=$(hostname)"
echo "cursor: ensuring repo at $REPO_DIR"
export GIT_TERMINAL_PROMPT=0
if [[ ! -d "$REPO_DIR/.git" ]]; then
  git clone "$CURSOR_REPO_URL" "$REPO_DIR"
else
  git -C "$REPO_DIR" fetch --all --prune
fi

if [[ -n "$${CURSOR_REPO_REF:-}" ]]; then
  git -C "$REPO_DIR" checkout "$CURSOR_REPO_REF"
  git -C "$REPO_DIR" pull --ff-only origin "$CURSOR_REPO_REF" || true
fi

echo "cursor: installing/updating agent CLI"
if ! command -v agent >/dev/null 2>&1; then
  curl https://cursor.com/install -fsS | bash
  export PATH="$HOME/.cursor/bin:$HOME/.local/bin:$PATH"
fi

AGENT_BIN="$(command -v agent || true)"
echo "cursor: agent path=$${AGENT_BIN:-missing}"
if [[ -z "$AGENT_BIN" ]]; then
  echo "cursor: agent binary not found on PATH=$PATH"
  exit 1
fi

ls -l "$AGENT_BIN"
file "$AGENT_BIN" || true

set +e
"$AGENT_BIN" help >/dev/null 2>&1
AGENT_HELP_EXIT=$?
"$AGENT_BIN" --version
AGENT_VERSION_EXIT=$?
set -e

echo "cursor: agent help exit=$AGENT_HELP_EXIT version exit=$AGENT_VERSION_EXIT"
echo "cursor: repo remote $(git -C "$REPO_DIR" remote get-url origin)"
echo "cursor: starting personal self-hosted worker"
echo "cursor: api key present=$$(if [[ -n "$${CURSOR_API_KEY:-}" ]]; then echo yes; else echo no; fi)"

WORKER_ARGS=(worker start --verbose --worker-dir "$REPO_DIR" --management-addr ":8080")

echo "cursor: exec $AGENT_BIN $${WORKER_ARGS[*]}"
exec "$AGENT_BIN" "$${WORKER_ARGS[@]}"
EOF
        destination = "local/startup.sh"
        perms       = "755"
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
        ssh_user     = "admin"
        ssh_password = "${SSH_PASSWORD}"
        show_ui      = false

        command = "/bin/bash"
        args    = ["/Volumes/My Shared Files/local/startup.sh"]

        directory {
          name = "local"
          path = "${NOMAD_TASK_DIR}"
        }
      }

      template {
        data        = <<EOH
SSH_PASSWORD={{ with nomadVar "nomad/jobs/cursor-self-hosted-worker" }}{{ .ssh_password }}{{ end }}
EOH
        destination = "secrets/file.env"
        env         = true
      }

      resources {
        cores  = 8
        memory = 10240
      }

      logs {
        max_files     = 5
        max_file_size = 10
      }
    }
  }
}
