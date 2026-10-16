# Local models with Ollama

**Level:** intermediate · **Shows:** private AI jobs, model maintenance, `missed: skip`, reading Noon history from a job

| Job | When | Does |
| --- | --- | --- |
| `models-pull` | Sunday 9am | `ollama pull` each model in `MODELS` |
| `model-warm` | weekdays 8:50am | Loads `llama3.2` with a 4h keep-alive; skipped if missed |
| `notes-digest` | Friday 4:30pm | Weekly summary of changed notes to `notes/digests/week-YYYY-Www.md` |
| `failures-digest` | weekdays 8am | One line per failed run in this project, from `noon history` + logs |

```sh
ollama serve                     # or the Ollama app
noon -C examples/ai/local-llm run models-pull
noon -C examples/ai/local-llm run model-warm
```

A local model has no tools, so these jobs are scripts that put everything the model needs in the
prompt. To use Ollama for prompt jobs, add a profile in user config (see
[agent profiles](../agent-profiles/user-config.yaml)):

```yaml
agents:
  ollama:
    command: [ollama, run, llama3.2]
    prompt: stdin
```

Swap `ollama run "$MODEL"` for `llm -m MODEL` or any CLI that reads a prompt on stdin.
