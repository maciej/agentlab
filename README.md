# AgentLab

AgentLab is a Pi-inspired agent harness playground for exploring how to build one in Go.

The project will experiment with Go-based agent orchestration, a Bubble Tea terminal UI, and sandboxed tool execution.

## Hello World

AgentLab currently has an Ollama hello-world command backed by a small in-memory session abstraction:

```sh
go run ./cmd/agentlab
```

Pass a custom prompt with `--prompt` or as positional text:

```sh
go run ./cmd/agentlab --prompt "Search the sandbox for aurora, then tell me the reported status."
```

By default, AgentLab snapshots `testdata/smoke-sandbox` and exposes these read-only Ollama tools to the model:

- `list_files`
- `read_file`
- `search_text`

Run the local tool smoke test against the configured model with:

```sh
make smoke
```

Run the Pi-based smoke test in an isolated Docker container with:

```sh
make smoke-pi
```

The Pi target copies only `testdata/smoke-sandbox` into a temporary container workspace, enables Pi's read-only
`read`, `grep`, `find`, and `ls` tools, and writes Pi's Ollama config to a temporary directory. It does not commit your
Ollama endpoint/model. Set `PI_OLLAMA_BASE_URL` and `PI_OLLAMA_MODEL` to override them; otherwise the script
reads `ollama.endpoint` and `ollama.model` from `AGENTLAB_CONFIG` or `~/.config/agentslab/config.yaml`. Set
`PI_SMOKE_TIMEOUT` to override the default `180s` Pi prompt timeout.

The CLI is built with Cobra and accepts `--config` for an explicit YAML config file:

```sh
go run ./cmd/agentlab --config ./config.yaml
```

Configuration is loaded from `~/.config/agentslab/config.yaml` on macOS by default. On other platforms it uses the
standard user config directory. Set `AGENTLAB_CONFIG` to point at a different config file.

```yaml
default_provider: local

providers:
  - name: local
    type: ollama
    settings:
      endpoint: http://localhost:11434
      model: qwen3-coder
      context_window: 98304
      # Optional. Use true/false for most thinking models, or low/medium/high for models that support levels.
      think: true

  - name: openai
    type: openai
    # Optional if OPENAI_API_KEY is set.
    api_key: sk-...
    settings:
      model: gpt-5.4
      # Optional. Defaults to https://api.openai.com/v1.
      base_url: https://api.openai.com/v1
```

Use a non-default configured provider with:

```sh
go run ./cmd/agentlab --provider openai --prompt "Say hello from AgentLab in one short sentence."
```
