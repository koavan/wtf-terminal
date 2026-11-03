# AI assistance

To work with your own Claude Code instead, see [Ask Claude](#ask-claude) ({{mac:⌘I|other:Ctrl+Shift+I}}).

wtf can send things you ask about to an AI and show its reply: you choose which AI. It works with any AI tool, set up in `config.toml`:

| Kind | What it covers |
|---|---|
| `anthropic` | the Claude API |
| `openai` | any OpenAI-compatible endpoint: OpenAI, Azure OpenAI, Gemini, Mistral, Groq, DeepSeek, OpenRouter, or a server on this computer (Ollama, LM Studio, vLLM, llama.cpp) |
| `command` | any command-line AI tool: `claude -p`, `codex exec`, `gemini`, `llm`, `aichat`… |

With no provider set up, AI features don't appear anywhere.

## Adding a provider

Open **Settings** ({{mac:⌘,|other:Ctrl+,}}) → **AI**, or **AI Providers…** in {{mac:the wtf menu|other:the window menu}} or the palette. Then **Add Provider…** and pick one:

- **Cloud APIs**: Claude, OpenAI, Gemini, OpenRouter, Groq, Mistral, DeepSeek. Paste your API key; each choice says where to create one.
- **On this computer**: Ollama, LM Studio. No key, and nothing leaves the machine.
- **Command-line tools**: Claude Code, Codex CLI, Gemini CLI, llm. They use their own login.
- **Other**: any OpenAI- or Anthropic-compatible endpoint (vLLM, llama.cpp, Azure OpenAI, a company gateway), or any program that reads a prompt on stdin.

The form is filled in from your choice; change anything:

- **Name**: how it's listed (letters, digits, `-` and `_`).
- **Model**: type it, or **Fetch Models** to pick from the provider's own list.
- **Base URL**: where requests go. Empty means Anthropic's or OpenAI's own API; OpenAI-compatible servers usually end in `/v1`.
- **API key**: saved in the {{mac:macOS Keychain|other:keyring (Secret Service: GNOME Keyring or KWallet)}} under "wtf AI", never in a file. Leave it empty when editing to keep the saved one. Or read it from an **environment variable** instead ({{mac:an app opened from the Finder or Dock doesn't see variables set in your shell's startup files, so the keychain is usually easier|other:the variable must be set where wtf was started}}).
- **Extra headers**: sent with every request, one `Name: value` per line. An Anthropic key made for the whole organization rather than one workspace needs `anthropic-workspace-id: wrkspc_…` (the ID is on the workspace's page at console.anthropic.com); gateways and proxies sometimes need one too. They're saved in `config.toml`, so never put a key here.
- **Command** (command-line tools): the program and its arguments, quoted as in a shell. wtf finds it on the focused shell's `PATH`.

**Edit…** changes a provider (renaming moves its key along), **Delete…** removes it and its saved key.

## Choosing and testing

Each provider's row shows its model, where requests go, whether it has a key, and whether it's the **Default** or the **Quick** one: the default answers most features, the quick one the small inline ones. **Use as Default** and **Use for Quick Features** change that; so do the palette's "AI: Use …" entries, one per provider.

**Test** sends a tiny request ("Reply with the single word OK") and shows the reply, how long it took, and the tokens or cost when the provider reports them. Errors show as the provider gave them: a wrong key, an unknown model, a tool that isn't logged in.

## Explain, commands from plain English, investigations

These go through [Ask Claude](#ask-claude): your own Claude Code in a split beside the shell, not the providers on this page.

- **Explain Failure… (Claude)**: right-click a failed command (or click its ✗ mark), or **Explain Last Failed Command…** in the palette and {{mac:the Edit menu|other:the window menu}}. The shell's Claude pane gets `/wtf:explain block N`: Claude reads that command and its output through wtf's tools, says why it likely failed, and can type a fix at your prompt for you to run.
- **Command from Plain English…**: from the palette, {{mac:the Edit menu|other:the window menu}} or the terminal's right-click menu. The Claude pane gets `/wtf:cmd ` for you to finish ("delete evicted pods in all namespaces"); Claude types one command at the shell's prompt, without pressing Enter.
- **Investigate…**: from the palette, the menus or the right-click menu. The Claude pane gets `/wtf:investigate ` for you to say what's wrong ("why is checkout-api crashlooping in staging?"). Claude looks with read-only commands from wtf's list, each shown in the shell as a normal command block, and asks you before every one.

The Claude pane opens if the shell doesn't have one yet. Text is typed only once Claude Code is ready for a prompt: never while it starts (its first question in a new folder is whether you trust it) or while it works.

## Review infrastructure changes

Before you apply a change, see what it will do. Right-click the output of `terraform plan` (or `tofu`, `terragrunt`), `kubectl diff` or `helm diff` → **Review Changes…**, use **Review Last Command's Changes…** in the palette, or right-click a saved plan in the Files tab → **Review Terraform Plan…**. A binary plan is read with `terraform show -json` (or `tofu`) in its folder, which changes nothing; a `terraform show -json` file is read directly.

wtf reads the output itself, without AI, and lists every change as a fact:

- `+` create, `~` change, `-/+` replace, `-` destroy, `<=` read, `→` moved;
- which attributes change, and which ones force a replacement (`engine_version forces replacement`);
- why, when the output says (not in configuration, tainted);
- changes to access control, marked **[IAM]**, **[Security group]**, **[RBAC]**, **[Network policy]** or **[Secret]** by resource type or kind.

The counts at the top are wtf's; Terraform's own line ("Plan: 2 to add, 1 to change, 2 to destroy") is shown under them, counting a replacement as one add and one destroy.

**Summarize with Claude** closes the review and asks the shell's Claude pane (`/wtf:review`): Claude reads the same facts through wtf's tools and writes a plain-English summary of what applying it will do, why each replacement and destruction happens, and what each access change grants or removes. See [Ask Claude](#ask-claude).

## In config.toml

Everything the Settings window saves is plain text in `config.toml` (**Open config.toml**), which you can also edit by hand:

```toml
ai-default = "claude"     # answers most features
ai-fast = "local"         # quick inline ones; ai-default when not set

[ai.providers.claude]
kind = "anthropic"
model = "claude-sonnet-5-5"

[ai.providers.local]
kind = "openai"
base-url = "http://localhost:11434/v1"   # Ollama
model = "qwen3:8b"

[ai.providers.codex]
kind = "command"
command = ["codex", "exec", "-"]
```

`api-key-env = "NAME"` reads a provider's key from an environment variable; `headers = { "anthropic-workspace-id" = "wrkspc_…" }` adds headers. With a single provider, `ai-default` isn't needed. Mistakes show as a Settings message and that provider is left out. Saving a provider from the window rewrites only its own table (comments inside it aren't kept).

## What is sent

- **Only when you ask.** Nothing calls an AI in the background, so having one set up costs no CPU, memory or network until you use it.
- **Secrets are removed first.** Every text goes through the same patterns as [Hide Secrets](#secrets): access keys, tokens, JWTs, passwords in URLs and settings, private keys become `[REDACTED]`. It's best effort and can miss something it doesn't recognize.
- **The first request to each provider is shown first**: the provider, model and destination, then the exact text, with how many secrets were replaced. Nothing goes until you press **Send**. It's shown again if you change where that provider sends to. Each AI feature can also show what it would send, on request.
- **`command` providers** get the text on their standard input. wtf runs them in a new, empty folder that's deleted afterwards, so a coding agent finds no project to work on. Beyond that, the tool's own settings apply: what it sends, where, and what it may do.
- **On this computer**: an `openai` provider at `localhost` sends nothing off the machine.

AI never runs a command that changes anything. Commands it suggests are typed into your shell for you to read, edit and run.

## Cost

Requests to paid APIs are billed to your own account by the provider. When the provider reports it, each reply shows the tokens used (and the cost, for providers like OpenRouter that report one). A request stops after 5 minutes; **Stop** ends it sooner.
