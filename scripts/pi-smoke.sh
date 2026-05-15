#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sandbox_src="${PI_SMOKE_SANDBOX:-$repo_root/testdata/smoke-sandbox}"
docker_image="${PI_DOCKER_IMAGE:-node:22-bookworm-slim}"
pi_package="${PI_PACKAGE:-@earendil-works/pi-coding-agent@0.74.0}"
prompt="${PI_SMOKE_PROMPT:-Search the sandbox for aurora, read the readiness note, and report only the codename and expected status.}"
smoke_timeout="${PI_SMOKE_TIMEOUT:-180s}"

config_path="${AGENTLAB_CONFIG:-$HOME/.config/agentslab/config.yaml}"

read_yaml_value() {
	local key="$1"
	local file="$2"
	awk -v key="$key" '
		$0 ~ /^ollama:[[:space:]]*$/ { in_ollama = 1; next }
		in_ollama && $0 ~ /^[^[:space:]]/ { in_ollama = 0 }
		in_ollama {
			line = $0
			sub(/^[[:space:]]+/, "", line)
			if (line ~ "^" key ":[[:space:]]*") {
				sub("^" key ":[[:space:]]*", "", line)
				gsub(/^["'\'']|["'\'']$/, "", line)
				print line
				exit
			}
		}
	' "$file"
}

normalize_url() {
	local url="$1"
	if [[ "$url" != http://* && "$url" != https://* ]]; then
		url="http://$url"
	fi
	url="${url%/}"
	if [[ "$url" != */v1 ]]; then
		url="$url/v1"
	fi
	printf '%s\n' "$url"
}

docker_reachable_url() {
	local url="$1"
	case "$url" in
		http://localhost:*|http://127.0.0.1:*)
			url="${url/http:\/\/localhost:/http:\/\/host.docker.internal:}"
			url="${url/http:\/\/127.0.0.1:/http:\/\/host.docker.internal:}"
			;;
	esac
	printf '%s\n' "$url"
}

endpoint="${PI_OLLAMA_BASE_URL:-${OLLAMA_BASE_URL:-${OLLAMA_HOST:-}}}"
model="${PI_OLLAMA_MODEL:-${OLLAMA_MODEL:-}}"

if [[ -z "$endpoint" || -z "$model" ]]; then
	if [[ -f "$config_path" ]]; then
		if [[ -z "$endpoint" ]]; then
			endpoint="$(read_yaml_value endpoint "$config_path" || true)"
		fi
		if [[ -z "$model" ]]; then
			model="$(read_yaml_value model "$config_path" || true)"
		fi
	fi
fi

if [[ -z "$endpoint" ]]; then
	printf 'PI_OLLAMA_BASE_URL is required, or set ollama.endpoint in %s\n' "$config_path" >&2
	exit 2
fi

if [[ -z "$model" ]]; then
	printf 'PI_OLLAMA_MODEL is required, or set ollama.model in %s\n' "$config_path" >&2
	exit 2
fi

if [[ ! -d "$sandbox_src" ]]; then
	printf 'Smoke sandbox does not exist: %s\n' "$sandbox_src" >&2
	exit 2
fi

command -v docker >/dev/null 2>&1 || {
	printf 'docker is required for make smoke-pi\n' >&2
	exit 2
}

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/agentlab-pi-smoke.XXXXXX")"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT

workspace="$tmpdir/workspace"
pi_config="$tmpdir/pi-config"
mkdir -p "$workspace" "$pi_config"
tar -C "$sandbox_src" -cf - . | tar -C "$workspace" -xf -

base_url="$(docker_reachable_url "$(normalize_url "$endpoint")")"
api_key="${PI_OLLAMA_API_KEY:-ollama}"

docker_args=(--rm -i)
if [[ "$(uname -s)" == "Linux" ]]; then
	docker_args+=(--add-host=host.docker.internal:host-gateway)
fi

printf 'Running Pi smoke test in Docker with a temporary copy of testdata/smoke-sandbox.\n'
printf 'Using Ollama endpoint: %s\n' "$endpoint"
printf 'Using Ollama model: %s\n' "$model"

docker run "${docker_args[@]}" \
	-v "$workspace:/workspace:ro" \
	-v "$pi_config:/pi-config" \
	-w /workspace \
	-e PI_CODING_AGENT_DIR=/pi-config \
	-e PI_OFFLINE=1 \
	-e PI_OLLAMA_BASE_URL="$base_url" \
	-e PI_OLLAMA_MODEL="$model" \
	-e PI_OLLAMA_API_KEY="$api_key" \
	-e PI_PACKAGE="$pi_package" \
	-e PI_SMOKE_PROMPT="$prompt" \
	-e PI_SMOKE_TIMEOUT="$smoke_timeout" \
	"$docker_image" \
	sh -eu -c '
		printf "Installing Pi and read-only tool dependencies...\n"
		if ! apt-get update >/tmp/pi-smoke-apt.log 2>&1 ||
			! apt-get install -y --no-install-recommends ca-certificates ripgrep fd-find >/tmp/pi-smoke-apt.log 2>&1; then
			cat /tmp/pi-smoke-apt.log >&2
			exit 1
		fi
		ln -sf /usr/bin/fdfind /usr/local/bin/fd
		if ! NPM_CONFIG_UPDATE_NOTIFIER=false npm_config_update_notifier=false npm_config_loglevel=silent \
			npm install -g "$PI_PACKAGE" --no-fund --no-audit >/tmp/pi-smoke-npm.log 2>&1; then
			cat /tmp/pi-smoke-npm.log >&2
			exit 1
		fi
		printf "Configuring Pi for Ollama...\n"
		node -e '"'"'
			const fs = require("fs");
			fs.mkdirSync("/pi-config", { recursive: true });
			fs.writeFileSync("/pi-config/models.json", JSON.stringify({
				providers: {
					ollama: {
						baseUrl: process.env.PI_OLLAMA_BASE_URL,
						api: "openai-completions",
						apiKey: process.env.PI_OLLAMA_API_KEY,
						models: [{ id: process.env.PI_OLLAMA_MODEL }]
					}
				}
			}, null, 2));
			fs.writeFileSync("/pi-config/settings.json", JSON.stringify({
				defaultProvider: "ollama",
				defaultModel: process.env.PI_OLLAMA_MODEL
			}, null, 2));
		'"'"'
		printf "Running Pi smoke prompt (timeout: %s)...\n" "$PI_SMOKE_TIMEOUT"
		set +e
		timeout --foreground "$PI_SMOKE_TIMEOUT" pi --offline --provider ollama --model "$PI_OLLAMA_MODEL" --api-key "$PI_OLLAMA_API_KEY" \
			--tools read,grep,find,ls \
			--no-context-files --no-extensions --no-skills --no-prompt-templates --no-themes --no-session \
			-p "$PI_SMOKE_PROMPT"
		status=$?
		set -e
		if [ "$status" -ne 0 ]; then
			if [ "$status" -eq 124 ]; then
				printf "Pi smoke prompt timed out after %s.\n" "$PI_SMOKE_TIMEOUT" >&2
			fi
			exit "$status"
		fi
	'
