#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sandbox_src="${PI_SMOKE_SANDBOX:-$repo_root/testdata/smoke-sandbox}"
docker_image="${PI_DOCKER_IMAGE:-node:22-bookworm-slim}"
pi_package="${PI_PACKAGE:-@earendil-works/pi-coding-agent@0.74.0}"
prompt="${PI_SMOKE_PROMPT:-Use grep to search for aurora in the sandbox, then read README.md and notes/tasks.md. Report only the codename and expected status.}"
smoke_timeout="${PI_SMOKE_TIMEOUT:-180s}"
event_log="${PI_SMOKE_EVENT_LOG:-1}"
preflight_timeout="${PI_OLLAMA_PREFLIGHT_TIMEOUT:-15}"
preflight_chat_timeout="${PI_OLLAMA_PREFLIGHT_CHAT_TIMEOUT:-45}"
preflight_timeout_seconds="${preflight_timeout%s}"
preflight_chat_timeout_seconds="${preflight_chat_timeout%s}"

config_path="${AGENTLAB_CONFIG:-$HOME/.config/agentlab/config.yaml}"

log() {
	printf '[%s] %s\n' "$(date -u '+%H:%M:%SZ')" "$*"
}

read_legacy_ollama_value() {
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

read_config_ollama_value() {
	local key="$1"
	local file="$2"
	if command -v ruby >/dev/null 2>&1; then
		ruby -ryaml -e '
			key = ARGV.fetch(0)
			file = ARGV.fetch(1)
			cfg = YAML.load_file(file) || {}
			providers = cfg["providers"] || []
			provider = providers.find { |p| p["name"] == "ollama" && p["type"] == "ollama" } ||
				providers.find { |p| p["type"] == "ollama" }
			value = provider && (provider["settings"] || {})[key]
			value ||= (cfg["ollama"] || {})[key]
			puts value if value
		' "$key" "$file"
		return
	fi
	read_legacy_ollama_value "$key" "$file"
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

preflight_ollama() {
	local url="$1"
	local model="$2"
	local api_key="$3"

	if [[ "${PI_SMOKE_SKIP_PREFLIGHT:-}" == "1" ]]; then
		log "Skipping Ollama preflight because PI_SMOKE_SKIP_PREFLIGHT=1."
		return 0
	fi
	if ! command -v ruby >/dev/null 2>&1; then
		log "Skipping Ollama preflight because ruby is not available on the host."
		return 0
	fi

	log "Preflight: checking Ollama model list at $url/models."
	ruby -rjson -rnet/http -rtimeout -ruri -e '
		base_url = ARGV.fetch(0)
		model = ARGV.fetch(1)
		api_key = ARGV.fetch(2)
		seconds = Float(ARGV.fetch(3))
		uri = URI("#{base_url}/models")
		response = Timeout.timeout(seconds) do
			http = Net::HTTP.new(uri.host, uri.port)
			http.use_ssl = uri.scheme == "https"
			http.open_timeout = seconds
			http.read_timeout = seconds
			request = Net::HTTP::Get.new(uri)
			request["Authorization"] = "Bearer #{api_key}"
			http.request(request)
		end
		abort "Ollama model list returned HTTP #{response.code}: #{response.body[0, 300]}" unless response.is_a?(Net::HTTPSuccess)
		ids = JSON.parse(response.body).fetch("data", []).map { |item| item["id"] }.compact
		unless ids.include?(model)
			abort "Ollama model #{model.inspect} is not listed. Available models: #{ids.join(", ")}"
		end
	' "$url" "$model" "$api_key" "$preflight_timeout_seconds"
	log "Preflight: model is listed by Ollama."

	if [[ "$preflight_chat_timeout_seconds" == "0" ]]; then
		log "Skipping Ollama chat preflight because PI_OLLAMA_PREFLIGHT_CHAT_TIMEOUT=0."
		return 0
	fi

	log "Preflight: sending tiny chat request to warm/check model (timeout: ${preflight_chat_timeout}s)."
	ruby -rjson -rnet/http -rtimeout -ruri -e '
		begin
			base_url = ARGV.fetch(0)
			model = ARGV.fetch(1)
			api_key = ARGV.fetch(2)
			seconds = Float(ARGV.fetch(3))
			uri = URI("#{base_url}/chat/completions")
			body = JSON.dump({
				model: model,
				stream: false,
				messages: [{ role: "user", content: "Reply with exactly: ok" }]
			})
			response = Timeout.timeout(seconds) do
				http = Net::HTTP.new(uri.host, uri.port)
				http.use_ssl = uri.scheme == "https"
				http.open_timeout = seconds
				http.read_timeout = seconds
				request = Net::HTTP::Post.new(uri)
				request["Authorization"] = "Bearer #{api_key}"
				request["Content-Type"] = "application/json"
				request.body = body
				http.request(request)
			end
			abort "Ollama chat preflight returned HTTP #{response.code}: #{response.body[0, 500]}" unless response.is_a?(Net::HTTPSuccess)
		rescue Timeout::Error
			abort "Ollama chat preflight timed out after #{seconds}s. The model may not be loaded or responsive."
		end
	' "$url" "$model" "$api_key" "$preflight_chat_timeout_seconds"
	log "Preflight: tiny chat request completed."
}

endpoint=""
endpoint_source=""
model=""
model_source=""

if [[ -n "${PI_OLLAMA_BASE_URL:-}" ]]; then
	endpoint="$PI_OLLAMA_BASE_URL"
	endpoint_source="PI_OLLAMA_BASE_URL"
elif [[ -n "${OLLAMA_BASE_URL:-}" ]]; then
	endpoint="$OLLAMA_BASE_URL"
	endpoint_source="OLLAMA_BASE_URL"
elif [[ -n "${OLLAMA_HOST:-}" ]]; then
	endpoint="$OLLAMA_HOST"
	endpoint_source="OLLAMA_HOST"
fi

if [[ -n "${PI_OLLAMA_MODEL:-}" ]]; then
	model="$PI_OLLAMA_MODEL"
	model_source="PI_OLLAMA_MODEL"
elif [[ -n "${OLLAMA_MODEL:-}" ]]; then
	model="$OLLAMA_MODEL"
	model_source="OLLAMA_MODEL"
fi

if [[ -z "$endpoint" || -z "$model" ]]; then
	if [[ -f "$config_path" ]]; then
		if [[ -z "$endpoint" ]]; then
			endpoint="$(read_config_ollama_value endpoint "$config_path" || true)"
			if [[ -n "$endpoint" ]]; then
				endpoint_source="$config_path"
			fi
		fi
		if [[ -z "$model" ]]; then
			model="$(read_config_ollama_value model "$config_path" || true)"
			if [[ -n "$model" ]]; then
				model_source="$config_path"
			fi
		fi
	fi
fi

if [[ -z "$endpoint" ]]; then
	printf 'PI_OLLAMA_BASE_URL is required, or configure an ollama provider settings.endpoint in %s\n' "$config_path" >&2
	exit 2
fi

if [[ -z "$model" ]]; then
	printf 'PI_OLLAMA_MODEL is required, or configure an ollama provider settings.model in %s\n' "$config_path" >&2
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

api_key="${PI_OLLAMA_API_KEY:-ollama}"
host_base_url="$(normalize_url "$endpoint")"
base_url="$(docker_reachable_url "$host_base_url")"

docker_args=(--rm -i)
if [[ "$(uname -s)" == "Linux" ]]; then
	docker_args+=(--add-host=host.docker.internal:host-gateway)
fi

log "Running Pi smoke test in Docker with a temporary copy of testdata/smoke-sandbox."
log "Using Ollama endpoint: $endpoint"
log "Ollama endpoint source: $endpoint_source"
log "Using Ollama model: $model"
log "Ollama model source: $model_source"
log "Using Pi package: $pi_package"
log "Using Docker image: $docker_image"
log "Using config path: $config_path"
log "Using host Ollama base URL: $host_base_url"
log "Using container Ollama base URL: $base_url"
log "Using smoke timeout: $smoke_timeout"
log "Using preflight timeouts: model-list=${preflight_timeout}s chat=${preflight_chat_timeout}s"
log "Using Pi event logging: $event_log"
preflight_ollama "$host_base_url" "$model" "$api_key"
log "Starting Docker container."

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
	-e PI_SMOKE_EVENT_LOG="$event_log" \
	"$docker_image" \
	sh -eu -c '
		log() {
			printf "[%s] %s\n" "$(date -u "+%H:%M:%SZ")" "$*"
		}

		log "Container started."
		log "Installing OS dependencies: ca-certificates ripgrep."
		log "Running apt-get update."
		if ! apt-get update >/tmp/pi-smoke-apt.log 2>&1; then
			cat /tmp/pi-smoke-apt.log >&2
			exit 1
		fi
		log "Running apt-get install."
		if ! apt-get install -y --no-install-recommends ca-certificates ripgrep >/tmp/pi-smoke-apt.log 2>&1; then
			cat /tmp/pi-smoke-apt.log >&2
			exit 1
		fi
		log "Installing Pi package: $PI_PACKAGE."
		if ! NPM_CONFIG_UPDATE_NOTIFIER=false npm_config_update_notifier=false npm_config_loglevel=silent \
			npm install -g "$PI_PACKAGE" --no-fund --no-audit >/tmp/pi-smoke-npm.log 2>&1; then
			cat /tmp/pi-smoke-npm.log >&2
			exit 1
		fi
		log "Pi package installed."
		log "Checking required tool binaries."
		command -v pi >/dev/null
		command -v rg >/dev/null
		log "Configuring Pi for Ollama."
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
		log "Pi config written to temporary container config directory."
		log "Running Pi smoke prompt (timeout: $PI_SMOKE_TIMEOUT)."
		cat > /tmp/pi-smoke-events.js <<'"'"'NODE'"'"'
const readline = require("readline");

let turn = 0;
let assistant = 0;
let sawFinalText = false;

function compact(value, max = 500) {
	const text = String(value ?? "").replace(/\s+/g, " ").trim();
	return text.length > max ? text.slice(0, max - 3) + "..." : text;
}

function renderContent(content) {
	if (!Array.isArray(content)) return "";
	const parts = [];
	for (const item of content) {
		if (item.type === "text" && item.text) parts.push(item.text);
		if (item.type === "toolCall") parts.push(`[tool ${item.name} ${JSON.stringify(item.arguments || {})}]`);
	}
	return compact(parts.join(" "));
}

const rl = readline.createInterface({ input: process.stdin });
rl.on("line", (line) => {
	let event;
	try {
		event = JSON.parse(line);
	} catch {
		console.log(`[pi:event] non-json: ${compact(line)}`);
		return;
	}

	if (event.type === "turn_start") {
		console.log(`[pi:event] turn ${++turn} started`);
		return;
	}
	if (event.type === "tool_execution_start") {
		console.log(`[pi:event] tool start: ${event.toolName} ${JSON.stringify(event.args || {})}`);
		return;
	}
	if (event.type === "tool_execution_end") {
		const text = renderContent(event.result && event.result.content);
		console.log(`[pi:event] tool end: ${event.toolName} error=${Boolean(event.isError)}${text ? ` ${text}` : ""}`);
		return;
	}
	if (event.type === "message_end" && event.message && event.message.role === "assistant") {
		assistant += 1;
		const text = renderContent(event.message.content);
		console.log(`[pi:event] assistant ${assistant} ended stop=${event.message.stopReason || "unknown"}${text ? ` ${text}` : ""}`);
		if (event.message.stopReason !== "toolUse" && text) {
			sawFinalText = true;
			console.log(text);
		}
		return;
	}
	if (event.type === "turn_end") {
		console.log(`[pi:event] turn ${turn} ended toolResults=${(event.toolResults || []).length}`);
		return;
	}
	if (event.type === "error") {
		console.log(`[pi:event] error ${compact(JSON.stringify(event))}`);
	}
});

process.on("exit", () => {
	if (!sawFinalText) {
		console.log("[pi:event] stream ended without final assistant text");
	}
});
NODE
		set +e
		if [ "$PI_SMOKE_EVENT_LOG" = "1" ]; then
			rm -f /tmp/pi-smoke-events.fifo
			mkfifo /tmp/pi-smoke-events.fifo
			node /tmp/pi-smoke-events.js < /tmp/pi-smoke-events.fifo &
			filter_pid=$!
			timeout --foreground "$PI_SMOKE_TIMEOUT" pi --offline --provider ollama --model "$PI_OLLAMA_MODEL" --api-key "$PI_OLLAMA_API_KEY" \
				--tools read,grep,ls \
				--no-context-files --no-extensions --no-skills --no-prompt-templates --no-themes --no-session \
				--mode json \
				-p "$PI_SMOKE_PROMPT" > /tmp/pi-smoke-events.fifo
			status=$?
			wait "$filter_pid" || true
			rm -f /tmp/pi-smoke-events.fifo
		else
			timeout --foreground "$PI_SMOKE_TIMEOUT" pi --offline --provider ollama --model "$PI_OLLAMA_MODEL" --api-key "$PI_OLLAMA_API_KEY" \
				--tools read,grep,ls \
				--no-context-files --no-extensions --no-skills --no-prompt-templates --no-themes --no-session \
				-p "$PI_SMOKE_PROMPT"
			status=$?
		fi
		set -e
		if [ "$status" -ne 0 ]; then
			if [ "$status" -eq 124 ]; then
				log "Pi smoke prompt timed out after $PI_SMOKE_TIMEOUT." >&2
			fi
			exit "$status"
		fi
		log "Pi smoke prompt completed."
	'
