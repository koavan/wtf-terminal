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

## Explain a failed command

Right-click a failed command (or click its ✗ mark) → **Explain Failure…**, or **Explain Last Failed Command…** in the palette and {{mac:the Edit menu|other:the window menu}} for the focused pane's last command. The default provider gets the facts wtf already has:

- the command, its exit code (with what the shell means by 126, 127 and the signal codes above 128) and how long it ran;
- the end of its output: the last 150 lines;
- the folder (your home folder as `~`), the system and shell;
- the shell's Kubernetes context and namespace, AWS profile, region and account, Google Cloud project, Terraform workspace and git branch, as the address bar shows them;
- the versions of well-known tools the command ran (`kubectl`, `aws`, `terraform`, `helm`, `docker`, `git`, `node`, `python3`…), asked from the shell's `PATH` and folder. Only tools on that list are asked; any other program is never run.

The reply streams in: the likely cause, then a fix command when one helps. **Type Fix at Prompt** puts it at the shell's prompt without pressing Enter, so you read, edit and run it yourself. **Copy** copies the explanation and the fix; **What's Sent** shows exactly what went to the provider; **Close** stops a reply still coming in.

## Command from plain English

Choose **Command from Plain English…** from the palette, {{mac:the Edit menu|other:the window menu}} or the terminal's right-click menu. Describe what you want ("delete evicted pods in all namespaces", "biggest files under this folder", "which process holds port 8080") and press Return.

The quick provider (`ai-fast`, else the default) gets your words with the focused shell's facts: folder, system and shell, its Kubernetes context and namespace, AWS profile and region, Google Cloud project, Terraform workspace, git branch, and which well-known tools are installed (looked up on the `PATH`, nothing is run). It answers with one command for that shell, written for your system's flavour of the tools, and a sentence about what it does. Names it would have to guess come back as `<placeholders>`.

- **Return** again (or **Insert at Prompt**) types the command at the prompt without pressing Enter: read it, edit it, run it.
- Change the words and press **Return** to ask again.
- **Explain Flags** asks what each part does: every program, subcommand, flag and argument, one per line.
- **What's Sent** shows exactly what went to the provider. **Esc** stops a reply, or closes the bar.

## Review infrastructure changes

Before you apply a change, see what it will do. Right-click the output of `terraform plan` (or `tofu`, `terragrunt`), `kubectl diff` or `helm diff` → **Review Changes…**, use **Review Last Command's Changes…** in the palette, or right-click a saved plan in the Files tab → **Review Terraform Plan…**. A binary plan is read with `terraform show -json` (or `tofu`) in its folder, which changes nothing; a `terraform show -json` file is read directly.

wtf reads the output itself, without AI, and lists every change as a fact:

- `+` create, `~` change, `-/+` replace, `-` destroy, `<=` read, `→` moved;
- which attributes change, and which ones force a replacement (`engine_version forces replacement`);
- why, when the output says (not in configuration, tainted);
- changes to access control, marked **[IAM]**, **[Security group]**, **[RBAC]**, **[Network policy]** or **[Secret]** by resource type or kind.

The counts at the top are wtf's; Terraform's own line ("Plan: 2 to add, 1 to change, 2 to destroy") is shown under them, counting a replacement as one add and one destroy.

With an AI set up, the default provider then gets these facts, the shell's context and the start of the output, and writes a plain-English summary: what applying it will do, why each replacement and destruction happens, and what each access change grants or removes. Values a plan file marks sensitive aren't sent. Without an AI, you still get the facts.

## Investigate an incident

Ask why something is broken ("why is checkout-api crashlooping in staging?") and let the AI find out. Open the **Investigate** tab in the side pane (the stethoscope{{mac:|other: / search}} icon, shown once an AI is set up), **Investigate (AI)…** in the palette, or right-click the terminal. Type the question and press Return.

The default provider gets your question, the focused shell's facts (folder, Kubernetes context and namespace, AWS profile and region, installed tools) and the last three commands you ran in it, with the end of their output. When those already answer the question (a command that just failed), it answers without running anything; so it does for a general question ("is it possible to back up Route 53?", "how do I…"); for a single failed command, **Explain Failure…** is quicker. Otherwise it works step by step.

**Nothing runs without you.** Each command it wants is first checked by wtf against a read-only list, then shown in the tab with why it wants it, marked *waiting for you*. **Run** types it into **that shell**, where it shows as a normal command block, uses exactly your shell's credentials, and lands in its history; **Skip** declines it, and the AI carries on without it (it's told not to ask for it again). Return in the terminal never approves a command: only a click on **Run** does. The tab lists every step: click one to scroll the terminal to it. When it has enough evidence it writes a **finding** that cites the steps ([1], [2]…), and what to do next.

What it may run:

- `kubectl` get, describe, logs, events, top, explain, api-resources, version, cluster-info, auth can-i, rollout status/history
- `helm` history, status, list, get notes/metadata, show chart
- `aws` describe-\*, list-\*, get-\*, sts get-caller-identity, s3 ls, logs filter-log-events/tail
- `gcloud` … list / describe; `docker` ps, images, inspect, logs, stats, top; `dig`, `host`, `nslookup`; `curl -I`; `ping`

Everything else is refused, and the tab shows the refusal and why. So are flags that would switch context or credentials (`--context`, `--kubeconfig`, `--profile`, `--as`), never end (`-f`, `--watch`, `--follow`), or print secret values (`kubectl get secret -o yaml`, `helm get values`, `get-secret-value`, `--with-decryption`). wtf adds what keeps a command finite: `--tail=200` for logs, `--no-cli-pager` for aws, `--max-time 10` for curl, `-c 3` for ping. The AI names a program and its arguments, never shell text: wtf quotes each argument itself, so nothing it writes can become a pipe, redirect or second command.

- **Limits**: at most 12 commands and 250,000 tokens per investigation; then it gives its finding from what it has. A command still running after 60 seconds gets Ctrl-C.
- **Stop** ends it at any time, also while a command waits for you. The shell is in use while a command you approved runs: don't type in it meanwhile.
- **A fix is never run.** When the finding suggests one (a rollback, a restart, a scale), **Type Fix at Prompt** puts it at the shell's prompt for you to read and run.
- It needs a provider that can call tools: the Claude API or an OpenAI-compatible endpoint (most local models served by Ollama or LM Studio can, if the model supports tools). Command-line AI tools can't, and the tab says so.
- Output goes through Hide Secrets' redaction before it's sent, like everything else; the first request to a provider shows what will be sent.

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

AI never runs a command that changes anything. Commands it suggests are typed into your shell for you to read, edit and run; the investigator asks before each read-only command it wants to run.

## Cost

Requests to paid APIs are billed to your own account by the provider. When the provider reports it, each reply shows the tokens used (and the cost, for providers like OpenRouter that report one). A request stops after 5 minutes; **Stop** ends it sooner.
