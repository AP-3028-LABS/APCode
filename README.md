# ⚡ APCode

### Offline-first AI coding agent for your terminal.

**APCode** is a lightweight, developer-focused AI coding agent that runs directly inside your terminal.
It understands your project, works with local models, provides an interactive TUI, and is designed to make AI-assisted development possible even on modest hardware.

> **Your terminal. Your code. Your model. Your agent.**

---

<div align="center">

### 🚀 APCode v0.1.7

**AI-powered coding • Local-first • Terminal-native • Cross-platform**

[![Release](https://img.shields.io/github/v/release/anshulchikhale30-p/APCode?style=for-the-badge\&logo=github)](https://github.com/anshulchikhale30-p/APCode/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/anshulchikhale30-p/APCode/ci.yml?style=for-the-badge\&label=CI)](https://github.com/anshulchikhale30-p/APCode/actions)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge\&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)](LICENSE)

</div>

---

## 🖥️ What is APCode?

APCode turns your terminal into an **AI coding workspace**.

Instead of repeatedly switching between:

```text
Terminal
   ↓
Editor
   ↓
AI Chat
   ↓
Terminal
   ↓
Documentation
```

APCode brings the interaction into one place:

```text
                 ┌──────────────────────┐
                 │       APCode         │
                 │   AI Coding Agent    │
                 └──────────┬───────────┘
                            │
              ┌─────────────┼─────────────┐
              ↓             ↓             ↓
          Your Code       Tools         Models
              │             │             │
              └─────────────┼─────────────┘
                            ↓
                     Project Changes
```

Ask APCode to understand, analyze, modify, or test your project.

---

# ✨ Experience APCode

Run:

```bash
apcode
```

You get a terminal-native workspace with:

```text
                         APCode
                         v0.1.7

Go · 126 files · Git: main


╭──────────────────────────────────────────────────────────────╮
│ › Ask anything...                                           │
│                                                              │
│   ollama · Qwen2.5-Coder 7B Q4 (Qwen) · q4                  │
╰──────────────────────────────────────────────────────────────╯

     enter  send       ctrl+c  cancel
     ctrl+p commands   tab     agents


~/APCode:main                                      v0.1.7

› Fix the authentication bug


APCode
────────────────────────────────────────────────────────────
Hello! I found the authentication flow and I'm analyzing it...
────────────────────────────────────────────────────────────

Enter ↵ send                         ollama · Qwen2.5-Coder 7B Q4
```

The interface is designed to stay **clean, readable, and keyboard-first**.

---

# 🧠 What makes APCode different?

### ⚡ Terminal-native

No heavyweight desktop application.

APCode lives where developers already work:

```text
Terminal
```

### 📴 Local-first

APCode is designed around local inference.

Use local models through supported runtimes instead of requiring every coding request to leave your machine.

### 💻 Hardware-aware

APCode detects your environment and can help determine what model your machine can realistically run.

```text
CPU
RAM
GPU
VRAM
Runtime
Model
```

### 🧩 Project-aware

APCode detects information about the project you're working inside.

For example:

```text
Go · 126 files · Git: main
```

or:

```text
Python · 24 files · Git: clean
```

### 🎨 Modern TUI

APCode isn't just a collection of commands.

It provides:

* Interactive prompt
* Project context
* Model indicator
* Command palette
* Agent activity
* Status information
* Spinners
* Semantic output
* Responsive terminal layouts

---

# 🤖 AI Coding Workflow

A typical workflow looks like:

```text
       User
        │
        │  "Fix the login bug"
        ▼
   ┌───────────┐
   │   APCode  │
   └─────┬─────┘
         │
         ▼
  Detect project
         │
         ▼
   Inspect files
         │
         ▼
   Search relevant code
         │
         ▼
     AI reasoning
         │
         ▼
   Plan modification
         │
         ▼
    Edit files
         │
         ▼
      Run tests
         │
         ▼
     Show result
```

The goal is simple:

> **Tell APCode what you want. Let the agent handle the development workflow.**

---

# 🛠️ Features

| Feature                    | Status |
| -------------------------- | ------ |
| 🖥️ Interactive TUI        | ✅      |
| 🤖 AI coding agent         | ✅      |
| 🧠 Local model support     | ✅      |
| 📁 Project detection       | ✅      |
| 💻 Hardware detection      | ✅      |
| ⚡ Hardware benchmarking    | ✅      |
| 🎯 Model recommendation    | ✅      |
| 🎨 Semantic TUI styling    | ✅      |
| 🌀 Activity/spinner UI     | ✅      |
| 📊 Project status          | ✅      |
| 🌈 `--no-color` mode       | ✅      |
| 🪟 Windows                 | ✅      |
| 🐧 Linux                   | ✅      |
| 🍎 macOS                   | ✅      |
| 📦 Cross-platform releases | ✅      |
| 🐳 Docker                  | ✅      |
| 📦 npm wrapper             | ✅      |
| 🍺 Homebrew                | ✅      |
| 🥄 Scoop                   | ✅      |

---

# 🚀 Installation

## Windows

PowerShell:

```powershell
irm https://raw.githubusercontent.com/anshulchikhale30-p/APCode/main/install.ps1 | iex
```

Then:

```powershell
apcode
```

---

## Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/anshulchikhale30-p/APCode/main/install.sh | bash
```

Then:

```bash
apcode
```

---

## Go

If you already have Go installed:

```bash
go install github.com/anshulchikhale30-p/APCode/cmd/apcode@latest
```

Then:

```bash
apcode
```

---

## Build from source

```bash
git clone https://github.com/anshulchikhale30-p/APCode.git
cd APCode

go build -o apcode ./cmd/apcode
```

Run:

```bash
./apcode
```

Windows:

```powershell
.\apcode.exe
```

---

# 🎮 Commands

Start the interactive workspace:

```bash
apcode
```

Useful commands:

```text
/help
/models
/recommend
/status
/benchmark
/clear
/exit
```

You can also use the direct CLI commands:

```bash
apcode --version

apcode models

apcode models installed

apcode models info <id>

apcode recommend

apcode benchmark
```

---

# ⌨️ Keyboard Controls

Inside the interactive TUI:

```text
Enter       Send request
Ctrl+C      Cancel operation
Ctrl+P      Commands
Tab         Switch agents
↑ / ↓       Navigate history
```

The interface is designed for keyboard-first development.

---

# 🧠 Models

APCode uses its model/runtime layer to work with available local models.

Example:

```text
ollama · Qwen2.5-Coder 7B Q4
```

If a model isn't available, APCode should tell you instead of pretending it is.

Example:

```text
⚠ No local model installed
```

Check models:

```bash
apcode models
```

Get a recommendation:

```bash
apcode recommend
```

---

# 📊 Hardware Benchmark

APCode includes a lightweight benchmark system to help understand your machine.

Run:

```bash
apcode benchmark
```

Example:

```text
APCode Benchmark

CPU
    7M ops/sec

Memory
    19K MiB/s

Runtime
    Native

Status
    Ready
```

This information can help guide local-model selection.

---

# 🔍 Project Awareness

Run APCode from inside your project:

```bash
cd my-project
apcode
```

APCode can detect project information such as:

```text
Language
Files
Git repository
Current branch
Working tree state
```

Example:

```text
Python · 24 files · Git: main
```

This gives the agent useful context before you ask it to work.

---

# 📋 Activity UI

When APCode performs work, operations are represented with semantic states:

```text
◐ Working
✓ Success
⚠ Warning
✗ Error
→ Action
```

Example:

```text
APCode

◐ Analyzing project...
✓ Project detected
✓ Found authentication files

◐ Reading src/auth/login.py
◐ Planning changes
◐ Editing src/auth/login.py

✓ File updated

◐ Running tests
✓ 18 tests passed
```

The important principle:

> **Activity should represent real operations — never fake tool calls.**

---

# 🎨 Responsive Terminal UI

APCode adapts to different terminal widths.

```text
< 80 columns
    Compact UI

80–119 columns
    Normal UI

≥ 120 columns
    Expanded UI
```

The goal is to keep the interface readable without overflowing the terminal.

---

# 🌈 No-Color Mode

APCode supports terminals where ANSI styling isn't desirable.

```bash
apcode --no-color
```

You can also use:

```bash
NO_COLOR=1 apcode
```

The interface remains usable without relying on color.

---

# ☁️ Free Cloud Models

APCode supports optional free cloud models through compatible OpenAI-compatible providers.

## Key Points

- **Internet is required** for cloud inference. Local/Ollama models remain available offline.
- **Users must provide their own provider credentials** where required.
- **Free model availability can change** without notice.
- **APCode does not provide or redistribute third-party API credentials.**

## Configuration

Set environment variables before running APCode:

```bash
export APCode_FREE_ENABLED=true
export APCode_FREE_API_KEY=your-api-key-here
export APCode_FREE_BASE_URL=https://your-provider-endpoint.com/v1
```

Or on Windows PowerShell:

```powershell
$env:APCode_FREE_ENABLED="true"
$env:APCode_FREE_API_KEY="your-api-key-here"
$env:APCode_FREE_BASE_URL="https://your-provider-endpoint.com/v1"
```

## Usage

List free cloud models:

```bash
apcode models --free
```

Available free models are also visible in the TUI via `/models`.

## Important Notes

- Free cloud functionality is **optional** — APCode works fully without it.
- Ollama/local model support is **never affected** by cloud configuration.
- API keys are read only from environment variables; they are never stored or logged.
- Do not claim unlimited free inference — provider limits may apply.

---

# 🏗️ Architecture

APCode keeps the terminal interface separated from the core systems.

```text
APCode
│
├── cmd/
│   └── apcode/
│
├── internal/
│   ├── agent/
│   ├── benchmark/
│   ├── codeintel/
│   ├── context/
│   ├── hardware/
│   ├── localmodel/
│   ├── model/
│   ├── recommendation/
│   ├── runtime/
│   ├── tools/
│   └── tui/
│       ├── app.go
│       ├── welcome.go
│       ├── prompt.go
│       ├── commands.go
│       ├── activity.go
│       ├── spinner.go
│       ├── status.go
│       └── styles.go
│
├── npm/
├── homebrew/
├── scoop/
├── .github/
│   └── workflows/
│
├── Dockerfile
├── Makefile
└── .goreleaser.yaml
```

The TUI should remain responsible for presentation and interaction rather than business logic.

---

# 🧪 Development

Clone the repository:

```bash
git clone https://github.com/anshulchikhale30-p/APCode.git
cd APCode
```

Format:

```bash
go fmt ./...
```

Lint:

```bash
go vet ./...
```

Run tests:

```bash
go test ./...
```

Build:

```bash
go build ./...
```

Run locally:

```bash
go run ./cmd/apcode
```

---

# 🧪 Validation

Before releasing APCode:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

Test the CLI:

```bash
go run ./cmd/apcode --version
go run ./cmd/apcode --no-color
go run ./cmd/apcode models
go run ./cmd/apcode recommend
go run ./cmd/apcode benchmark
```

---

# 📦 Release

APCode uses automated release tooling for cross-platform builds.

Supported targets include:

```text
Linux
├── amd64
└── arm64

macOS
├── amd64
└── arm64

Windows
├── amd64
└── arm64
```

Release artifacts can be generated through the project's release workflow.

---

# 🐳 Docker

Build:

```bash
docker build -t apcode .
```

Run:

```bash
docker run -it apcode
```

---

# 🔐 Design Philosophy

APCode is built around a few principles:

### Local-first

Your development environment should not depend entirely on cloud infrastructure.

### Lightweight

A coding agent shouldn't require a massive desktop application just to interact with your project.

### Transparent

The agent should clearly communicate what it is doing.

### Reliable

Visual effects are secondary to correct behavior.

### Developer-first

Keyboard shortcuts, terminal compatibility, readable output, and predictable commands matter.

---

# 🗺️ Roadmap

APCode is actively evolving.

Possible future improvements include:

```text
□ Better agent planning
□ Richer code intelligence
□ Improved diff visualization
□ More local model integrations
□ Improved model benchmarking
□ Agent/tool permissions
□ Session persistence
□ Better project indexing
□ More terminal themes
□ Plugin/extensibility system
```

---

# 🤝 Contributing

Contributions are welcome.

1. Fork the repository.
2. Create a feature branch.
3. Make your changes.
4. Add tests.
5. Run:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

6. Open a pull request.

---

# 📄 License

APCode is released under the **MIT License**.

See [LICENSE](LICENSE).

---

<div align="center">

## ⚡ APCode

**Code locally. Think with AI. Build faster.**

```text
Your terminal
      +
Your project
      +
Your model
      ↓
    APCode
```

⭐ If APCode is useful to you, consider starring the repository.

</div>
