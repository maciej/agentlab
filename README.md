# AgentLab

AgentLab is a Pi-inspired agent harness playground for exploring how to build one in Go.

The project will experiment with Go-based agent orchestration, a Bubble Tea terminal UI, and sandboxed tool execution.

## Hello World

AgentLab currently has an Ollama hello-world command backed by a small in-memory session abstraction:

```sh
go run ./cmd/agentlab
```

The CLI is built with Cobra and accepts `--config` for an explicit YAML config file:

```sh
go run ./cmd/agentlab --config ./config.yaml
```

Configuration is loaded from `~/.config/agentslab/config.yaml` on macOS by default. On other platforms it uses the
standard user config directory. Set `AGENTLAB_CONFIG` to point at a different config file.

```yaml
# Local model provider.
provider: ollama

ollama:
  endpoint: http://100.69.186.98:11434
  model: gemma4:26b
  context_window: 32768
```
