#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [ "${PODLET_DEMO_LOADED:-0}" != "1" ] && [ -f "$repo_root/.env.op" ]; then
  if command -v op >/dev/null 2>&1; then
    echo "==> Loading .env.op through 1Password (secrets stay ephemeral)"
    export PODLET_DEMO_LOADED=1
    exec op run --env-file="$repo_root/.env.op" -- bash "$0" "$@"
  else
    echo "note: 'op' CLI not found; sourcing .env.op without secret resolution" >&2
    set -a
    # shellcheck disable=SC1091
    . "$repo_root/.env.op"
    set +a
  fi
fi

host="${PODLET_DEMO_HOST:-127.0.0.1}"
user="${PODLET_DEMO_USER:-$(whoami)}"
demo_port="${PODLET_DEMO_PORT:-18080}"
password="${PODLET_DEMO_PASSWORD:-}"
workspace="${repo_root}/.dev/demo"

for tool in go tofu ssh curl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: missing local tool '$tool'" >&2
    exit 1
  fi
done

echo "==> Building terraform-provider-podlet"
go build -o terraform-provider-podlet .

echo "==> Checking SSH access to $user@$host"
if [ -n "$password" ]; then
  if command -v sshpass >/dev/null 2>&1; then
    sshpass -p "$password" ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 \
      "$user@$host" 'true' || {
      echo "error: SSH login with the configured password failed" >&2
      exit 1
    }
  else
    echo "note: sshpass not installed; recording the host key and letting the provider authenticate"
    ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 \
      "$user@$host" 'true' >/dev/null 2>&1 || true
  fi
else
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 \
    "$user@$host" 'true' || {
    echo "error: passwordless SSH key authentication to $user@$host is required" >&2
    echo "set PODLET_DEMO_PASSWORD in .env.op to use password authentication instead" >&2
    exit 1
  }
fi

echo "==> Checking remote Podman"
ssh -o BatchMode=yes -o ConnectTimeout=10 "$user@$host" \
  'podman --version >/dev/null' || {
  echo "error: remote host needs Podman" >&2
  exit 1
}

echo "==> Checking user systemd lingering"
linger="$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$user@$host" \
  'loginctl show-user "$(id -un)" -p Linger --value' 2>/dev/null || echo no)"
if [ "$linger" != "yes" ]; then
  echo "error: user systemd lingering is disabled on $host" >&2
  echo "enable it with: sudo loginctl enable-linger $user" >&2
  exit 1
fi

key_path="${PODLET_DEMO_KEY_PATH:-}"
if [ -z "$key_path" ] && [ -z "$password" ]; then
  for candidate in id_ed25519 id_ecdsa id_rsa; do
    if [ -f "$HOME/.ssh/$candidate" ]; then
      key_path="~/.ssh/$candidate"
      break
    fi
  done
fi

echo "==> Preparing demo workspace in $workspace"
rm -rf "$workspace"
mkdir -p "$workspace"
cp examples/demo/main.tf "$workspace/main.tf"
cat > "$workspace/demo.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/xiantongc612/podlet" = "$repo_root"
  }
  direct {}
}
EOF

run_demo() {
  (cd "$workspace" && TF_CLI_CONFIG_FILE="$workspace/demo.tfrc" TF_VAR_password="$password" tofu "$@")
}

cleanup() {
  if [ "${PODLET_DEMO_KEEP:-0}" != "1" ]; then
    echo
    echo "==> Destroying the hello demo"
    run_demo destroy -auto-approve \
      -var "host=$host" -var "user=$user" -var "private_key_path=$key_path" >/dev/null 2>&1 || true
  fi
  rm -rf "$workspace"
}
trap cleanup EXIT

echo "==> Initializing OpenTofu"
run_demo init -input=false >/dev/null 2>&1 || true

echo "==> Applying the hello demo"
run_demo apply -auto-approve \
  -var "host=$host" -var "user=$user" -var "private_key_path=$key_path"

echo "==> Waiting for the container to serve HTTP"
served=false
for _ in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:${demo_port}/" -o /dev/null; then
    served=true
    break
  fi
  sleep 2
done

if [ "$served" = true ]; then
  echo
  echo "==> Hello from your podlet-managed container =="
  curl -sf "http://127.0.0.1:${demo_port}/" | sed -n '1,8p'
else
  echo "warning: container did not respond on port $demo_port" >&2
  echo "inspect it with: ssh $user@$host systemctl --user status hello.service" >&2
fi

if [ "${PODLET_DEMO_KEEP:-0}" = "1" ]; then
  echo
  echo "==> Demo left running (PODLET_DEMO_KEEP=1)"
  echo "destroy it with: op run --env-file=.env.op -- tofu -chdir=.dev/demo destroy -auto-approve -var host=$host -var user=$user"
fi