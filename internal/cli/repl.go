// Package cli implements the interactive REPL for APCode.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"apcode/internal/benchmark"
	"apcode/internal/config"
	projectcontext "apcode/internal/context"
	"apcode/internal/hardware"
	"apcode/internal/localmodel"
	"apcode/internal/model"
	"apcode/internal/runtime"
	"apcode/internal/tools"
	"apcode/internal/tui"
	"apcode/internal/vision"
)

// REPL is the interactive terminal session.
type REPL struct {
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	Workspace   string
	IsGitRepo   bool
	GitBranch   string
	ProjectCtx  *projectcontext.Result
	Hardware    hardware.HardwareProfile
	Runtime     runtime.InferenceRuntime
	RuntimeName string
	Model       *model.ModelMetadata
	ModelDir    string
	Registry    *tools.Registry
	History     []Message
	Journal     *Journal
	NoColor     bool
	CmdHistory  []string // previously entered commands/prompts
	lastPlan    string
	reader      *bufio.Reader
	// Attachments holds images attached via /image or /attach for the next
	// prompt. Multiple images are allowed. Cleared after the prompt is sent
	// or on /clear.
	Attachments []ImageAttachment
}

// Message is a conversation turn.
type Message struct {
	Role    string
	Content string
}

// NewREPL creates a REPL with detected workspace and runtime.
func NewREPL(in io.Reader, out, errOut io.Writer) (*REPL, error) {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	ws, _ := os.Getwd()
	if ws == "" {
		ws = "."
	}
	absWs, _ := filepath.Abs(ws)

	isGit := false
	branch := ""
	if _, err := os.Stat(filepath.Join(absWs, ".git")); err == nil {
		isGit = true
		if data, err := os.ReadFile(filepath.Join(absWs, ".git", "HEAD")); err == nil {
			s := strings.TrimSpace(string(data))
			if strings.HasPrefix(s, "ref: refs/heads/") {
				branch = strings.TrimPrefix(s, "ref: refs/heads/")
			} else if len(s) >= 7 {
				branch = s[:7]
			}
		}
		if branch == "" {
			branch = "main"
		}
	} else {
		dir := absWs
		for dir != filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				isGit = true
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	var projResult *projectcontext.Result
	if root, err := projectcontext.DetectProjectRoot(absWs); err == nil {
		cfg := projectcontext.DefaultConfig()
		cfg.Root = root
		if res, err := projectcontext.WalkProject(root, cfg); err == nil {
			projResult = res
		}
	}

	hw, _ := hardware.Detect()

	session := ResolveSession()
	rt := session.Runtime
	rtName := session.RuntimeName
	mdl := session.Model
	modelDir := session.ModelDir

	reg, _ := tools.DefaultRegistryWithWorkspace(absWs)
	if reg == nil {
		reg = tools.DefaultRegistry()
	}
	tools.RegisterSpecTools(reg, absWs)

	journal := NewJournal()
	if wrapped, werr := wrapRegistryWithJournal(reg, absWs, journal); werr == nil {
		reg = wrapped
	}

	return &REPL{
		In:          in,
		Out:         out,
		ErrOut:      errOut,
		Workspace:   absWs,
		IsGitRepo:   isGit,
		GitBranch:   branch,
		ProjectCtx:  projResult,
		Hardware:    hw,
		Runtime:     rt,
		RuntimeName: rtName,
		Model:       mdl,
		ModelDir:    modelDir,
		Registry:    reg,
		History:     []Message{},
		Journal:     journal,
		reader:      bufio.NewReader(in),
	}, nil
}

// Run starts the interactive loop.
func (r *REPL) Run(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	defer func() {
		if r.interactiveOut() {
			tui.ResetTerminalBackground(r.Out)
		}
	}()

	r.printWelcome()

	if ctx == nil {
		ctx = context.Background()
	}
	if r.reader == nil {
		r.reader = bufio.NewReader(r.In)
	}
	reader := r.reader

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(r.Out, "\n"+tui.Muted("Session cancelled."))
			return ctx.Err()
		default:
		}

		boxWidth := tui.BoxWidth(r.uiWidth())
		// Show attachment chips above the input box when images are loaded (TUI chip integration).
		if len(r.Attachments) > 0 {
			mode := r.RuntimeName
			if mode == "" && r.Runtime != nil {
				mode = string(r.Runtime.Type())
			}
			if mode == "" {
				mode = "native"
			}
			modelName := ""
			if r.Model != nil {
				modelName = r.Model.ID
			}
			// Render chips • runtime • model for visibility in both CLI and TUI.
			status := tui.InputStatusLinesWithImages(mode, modelName, "", "", r.attachedPaths())
			// Fallback: at least show the chip line if status is empty.
			if status != "" {
				fmt.Fprintln(r.Out, tui.Muted("Attachments: ")+status)
			} else {
				fmt.Fprintln(r.Out, tui.Muted("Attachments: ")+tui.AttachmentChips(r.attachedPaths()))
			}
		}
		fmt.Fprintln(r.Out, tui.InputBoxTop(boxWidth))
		fmt.Fprint(r.Out, tui.InputBoxPrefix())

		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			lineCh <- line
		}()

		select {
		case <-sigCh:
			fmt.Fprintln(r.Out, "\n"+tui.InputBoxBottom(boxWidth))
			fmt.Fprintln(r.Out, "^C")
			fmt.Fprintln(r.Out, tui.Muted("(Ctrl+C) - type /exit to quit"))
			continue
		case err := <-errCh:
			if err == io.EOF {
				fmt.Fprintln(r.Out, tui.InputBoxBottom(boxWidth))
				fmt.Fprintln(r.Out, tui.Muted("Goodbye!"))
				return nil
			}
			fmt.Fprintf(r.ErrOut, "read error: %v\n", err)
			continue
		case line := <-lineCh:
			if !r.interactiveOut() {
				fmt.Fprintln(r.Out)
			}
			fmt.Fprintln(r.Out, tui.InputBoxBottom(boxWidth))
			// Handle raw keybindings that arrive as control characters
			// in cooked mode (user presses key then Enter) as well as
			// textual aliases for testing. This makes the footer hints
			// `enter send / ctrl+c cancel / ctrl+p commands / tab agents`
			// actually functional without requiring raw terminal mode.
			trimmedForKeys := strings.TrimSpace(line)
			// Tab (0x09) -> agents. In cooked mode Tab arrives as "\t\n"
			// when user presses Tab then Enter; also handle the word "tab" for tests.
			if line == "\t\n" || line == "\x09\n" || strings.EqualFold(trimmedForKeys, "tab") {
				r.handleTools()
				fmt.Fprintln(r.Out, tui.Muted("Tip: Tab shows agents/tools — also try /dashboard for the full split view"))
				continue
			}
			// Ctrl+P (0x10) -> commands palette
			if line == "\x10\n" || strings.EqualFold(trimmedForKeys, "ctrl+p") {
				r.printHelp()
				continue
			}
			// Ctrl+C as typed text (real Ctrl+C is SIGINT via sigCh)
			if strings.EqualFold(trimmedForKeys, "ctrl+c") {
				fmt.Fprintln(r.Out, tui.Muted("Interrupted. (Ctrl+C)"))
				continue
			}
			// Esc (0x1b) -> interrupt (like Ctrl+C)
			if line == "\x1b\n" || strings.EqualFold(trimmedForKeys, "esc") {
				fmt.Fprintln(r.Out, tui.Muted("Interrupted."))
				continue
			}
			input := strings.TrimSpace(line)
			if input == "" {
				continue
			}
			r.CmdHistory = append(r.CmdHistory, input)
			if strings.HasPrefix(input, "/") {
				shouldExit := r.handleSlashCommand(ctx, input)
				if shouldExit {
					fmt.Fprintln(r.Out, tui.Muted("Goodbye!"))
					return nil
				}
				continue
			}
			fmt.Fprintln(r.Out, tui.FooterHints("Enter ↵ send", r.modelIndicatorText(), r.uiWidth()))
			fmt.Fprintln(r.Out)
			r.History = append(r.History, Message{Role: "user", Content: input})
			r.Journal.BeginGroup()
			agentCtx, cancel := context.WithCancel(ctx)
			go func() {
				select {
				case <-sigCh:
					fmt.Fprintln(r.Out, "\n"+tui.Muted("Interrupted. (Ctrl+C)"))
					cancel()
					if r.Runtime != nil {
						_ = r.Runtime.Cancel(context.Background())
					}
				case <-agentCtx.Done():
					// Normal completion or upstream cancellation - stop watching.
				}
			}()
			response, err := r.runAgent(agentCtx, input)
			cancel()
			r.Journal.EndGroup()
			if err != nil {
				// Preserve attachments when blocked due to vision capability so user can retry after switching.
				if !errors.Is(err, vision.ErrNotVisionModel) {
					r.clearAttachments()
				}
				if err == context.Canceled {
					fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityWarning, "Cancelled."))
				} else {
					fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, err.Error()))
				}
				continue
			}
			// Clear image attachments automatically once the prompt is sent (spec).
			r.clearAttachments()
			r.History = append(r.History, Message{Role: "assistant", Content: response})
			fmt.Fprintf(r.Out, "\n%s\n", tui.Secondary("APCode"))
			fmt.Fprintf(r.Out, "%s\n\n", tui.ResponseBlock(response, r.uiWidth()))
		}
	}
}

// uiWidth returns the terminal width for layout decisions, defaulting to a
// safe value when output is not a terminal or the size cannot be detected.
func (r *REPL) uiWidth() int {
	if r.interactiveOut() {
		return tui.TerminalWidth()
	}
	return 80
}

// uiHeight returns the terminal height for full-viewport background fill.
func (r *REPL) uiHeight() int {
	if r.interactiveOut() {
		return tui.TerminalHeight()
	}
	return 0
}

// interactiveOut reports whether output is attached to a real terminal.
func (r *REPL) interactiveOut() bool {
	return tui.IsTerminalWriter(r.Out)
}

// printWelcome renders the APCode welcome screen from real session state.
// OpenCode-inspired: version + repo summary centered, woven logo, two-row
// input box with status segments inside, keybind hints, context-aware tip,
// and pinned status bar — no static slash-command list (moved to palette).
func (r *REPL) printWelcome() {
	width := r.uiWidth()
	height := r.uiHeight()
	// Whole-terminal background change when user types `apcode` — set the
	// terminal emulator background (OSC 11) and ensure the viewport is filled
	// with the dynamic 48;2 fill. This makes the entire canvas opencode-like,
	// not just the printed rows.
	if r.interactiveOut() {
		tui.WriteTerminalBackground(r.Out)
		// Clear with background so even areas outside the welcome content show the fill
		if bg := tui.GetBackgroundEscape(); bg != "" && tui.ColorsEnabled() {
			// Fill the screen with background by clearing; the welcome's per-line
			// BackgroundFill will keep it filled, but an initial clear ensures the
			// whole terminal viewport is painted on first run.
			fmt.Fprint(r.Out, bg)
		}
	}
	// derive mode / model / provider for the input box's second row
	mode := r.RuntimeName
	if mode == "" && r.Runtime != nil {
		mode = string(r.Runtime.Type())
	}
	if mode == "" {
		mode = "native"
	}
	modelName := ""
	provider := ""
	if r.Model != nil {
		modelName = r.Model.Name
		provider = string(r.Model.Provider)
	}
	hasModel := r.Model != nil
	// highlight: only when a model is present, surface one amber setting as example
	highlight := ""
	if hasModel {
		// e.g. show quantization or a highlighted setting — kept minimal
		if r.Model != nil && r.Model.Quantization != "" {
			highlight = string(r.Model.Quantization)
		}
	}
	fmt.Fprint(r.Out, tui.WelcomeScreen(tui.WelcomeOptions{
		Version:        config.Version,
		Commands:       tui.DefaultMenuCommands(), // kept for compat; WelcomeScreen ignores it
		ProjectLine:    r.projectLine(),
		Width:          width,
		Height:         height,
		Mode:           mode,
		ModelName:      modelName,
		Provider:       provider,
		Highlight:      highlight,
		HasModel:       hasModel,
		Workspace:      r.Workspace,
		GitBranch:      r.GitBranch,
		AttachedImages: r.attachedPaths(),
	}))
}

// projectLine builds the compact project summary from detected state.
func (r *REPL) projectLine() string {
	lang := "unknown"
	files := 0
	if r.ProjectCtx != nil {
		files = len(r.ProjectCtx.Files)
		max := 0
		for l, c := range r.ProjectCtx.Languages {
			if c > max {
				max = c
				lang = l
			}
		}
	}
	return tui.ProjectLine(lang, files, r.IsGitRepo, r.GitBranch)
}

// modelIndicatorText describes the selected runtime/model without ever
// claiming availability that does not exist.
func (r *REPL) modelIndicatorText() string {
	rtName := r.RuntimeName
	if rtName == "" && r.Runtime != nil {
		rtName = string(r.Runtime.Type())
	}
	modelName := ""
	if r.Model != nil {
		modelName = r.Model.Name
	}
	return tui.ModelIndicator(rtName, modelName)
}

func (r *REPL) handleSlashCommand(ctx context.Context, input string) bool {
	raw := strings.TrimSpace(input)
	parts := strings.Fields(raw) // preserve original case for path
	if len(parts) == 0 {
		return false
	}
	lowerBase := strings.ToLower(parts[0])
	switch lowerBase {
	case "/help", "/h", "/?":
		r.printHelp()
	case "/clear", "/cls":
		r.clearAttachments()
		fmt.Fprint(r.Out, "\033[H\033[2J")
		r.printWelcome()
	case "/image", "/attach":
		r.handleImageAttach(raw)
	case "/new", "/session":
		r.handleNewSession()
	case "/context", "/ctx":
		r.handleContext()
	case "/models":
		r.handleModels()
	case "/runtime", "/rt":
		r.handleRuntime()
	case "/diff":
		r.handleDiff(ctx)
	case "/git":
		r.handleDiff(ctx)
		r.handleStatus()
	case "/status", "/st":
		r.handleStatus()
	case "/benchmark", "/bench":
		r.handleBenchmark(ctx)
	case "/files":
		r.handleFiles()
	case "/search":
		r.handleSearch(ctx, input)
	case "/model":
		r.handleModel()
	case "/plan":
		r.handlePlan()
	case "/compact":
		r.handleCompact()
	case "/permissions":
		r.handlePermissions()
	case "/tools":
		r.handleTools()
	case "/rollback":
		r.handleRollback()
	case "/background", "/bg", "/theme":
		r.handleBackground(raw)
	case "/dashboard":
		r.handleDashboard()
	case "/todos":
		r.handleTodos()
	case "/exit", "/quit", "/q":
		return true
	default:
		fmt.Fprintf(r.Out, "%s Unknown command: %s (type /help)\n", tui.Warning("?"), input)
	}
	return false
}

func (r *REPL) printHelp() {
	fmt.Fprintln(r.Out, tui.Bold("APCode Commands"))
	cmds := []struct{ cmd, desc string }{
		{"/help", "Show this help"},
		{"/new", "New session (clears conversation)"},
		{"/models", "List available models (local + free cloud)"},
		{"/model", "Show the currently selected model"},
		{"/runtime", "Show runtime status"},
		{"/status", "Show project and system status"},
		{"/benchmark", "Run hardware benchmark"},
		{"/context", "Show project context summary"},
		{"/files [dir]", "List files via the agent's file tool"},
		{"/search <query>", "Search files in the workspace"},
		{"/image", "Attach image(s) using the system file picker when no path is given"},
		{"/image <path>", "Attach image by path (PNG, JPEG, WEBP, GIF, BMP)"},
		{"/attach <path>", "Alias for /image <path>"},
		{"/plan", "Show the plan from the current/last task"},
		{"/todos", "Show live todo checklist"},
		{"/dashboard", "Show two-column session dashboard"},
		{"/background #RRGGBB", "Set terminal background color (or /bg, /theme)"},
		{"/compact", "Compact conversation history"},
		{"/permissions", "Show the tool permission policy"},
		{"/tools", "List every registered agent tool + schema"},
		{"/git", "Show git diff + status"},
		{"/diff", "Show git diff"},
		{"/rollback", "Revert the last APCode change set"},
		{"/clear", "Clear screen and redraw welcome"},
		{"/exit", "Exit APCode"},
	}
	for _, c := range cmds {
		fmt.Fprintf(r.Out, "  %s %s\n", tui.Primary(fmt.Sprintf("%-16s", c.cmd)), tui.Muted(c.desc))
	}
	fmt.Fprintln(r.Out)
	fmt.Fprintln(r.Out, tui.Bold("Keyboard"))
	keys := []struct{ key, desc string }{
		{"Ctrl+C", "Cancel current operation / exit hint"},
	}
	for _, k := range keys {
		fmt.Fprintf(r.Out, "  %s %s\n", tui.Primary(fmt.Sprintf("%-16s", k.key)), tui.Muted(k.desc))
	}
	fmt.Fprintln(r.Out)
	fmt.Fprintln(r.Out, tui.Muted("Any other text is sent to the AI agent."))
}

// handleNewSession starts fresh: conversation history and plan are cleared
// (the journal is kept so /rollback can still revert earlier changes).
func (r *REPL) handleNewSession() {
	r.History = []Message{}
	r.lastPlan = ""
	r.CmdHistory = nil
	fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivitySuccess, "New session started."))
	r.printWelcome()
}

// handleBenchmark runs the hardware benchmark through the benchmark
// package and prints a compact result summary.
func (r *REPL) handleBenchmark(ctx context.Context) {
	sp := tui.NewSpinner(r.Out, "Running benchmark...", r.interactiveOut())
	sp.Ctx = ctx
	sp.Start()
	cfg := benchmark.DefaultConfig()
	runner := &benchmark.BenchmarkRunner{}
	res, err := runner.Run(ctx, r.Hardware, cfg)
	sp.Stop()
	if err != nil {
		fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Benchmark failed: "+err.Error()))
		return
	}
	if res.CPU.Success {
		fmt.Fprintf(r.Out, "%s\n", tui.ActivityLine(tui.ActivitySuccess, fmt.Sprintf("CPU: %.0f ops/sec", res.CPU.OperationsPerSec)))
	} else {
		fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityWarning, "CPU: unavailable"))
	}
	if res.Memory.Success {
		fmt.Fprintf(r.Out, "%s\n", tui.ActivityLine(tui.ActivitySuccess, fmt.Sprintf("Memory: %.2f MiB/s", res.Memory.BytesPerSec/(1024*1024))))
	} else {
		fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityWarning, "Memory: unavailable"))
	}
	if res.Storage.Success {
		fmt.Fprintf(r.Out, "%s\n", tui.ActivityLine(tui.ActivitySuccess, fmt.Sprintf("Storage: write %.2f MiB/s, read %.2f MiB/s",
			res.Storage.WriteBytesPerSec/(1024*1024), res.Storage.ReadBytesPerSec/(1024*1024))))
	} else {
		fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityWarning, "Storage: unavailable"))
	}
}

func (r *REPL) handleContext() {
	if r.ProjectCtx == nil {
		fmt.Fprintln(r.Out, tui.Warning("No project context available."))
		return
	}
	fmt.Fprintf(r.Out, "%s Project: %s\n", tui.Primary("Context:"), r.Workspace)
	fmt.Fprintf(r.Out, "  Files: %d\n", len(r.ProjectCtx.Files))
	fmt.Fprintf(r.Out, "  Languages: ")
	langs := []string{}
	for l, c := range r.ProjectCtx.Languages {
		langs = append(langs, fmt.Sprintf("%s (%d)", l, c))
	}
	if len(langs) > 0 {
		fmt.Fprintln(r.Out, strings.Join(langs, ", "))
	} else {
		fmt.Fprintln(r.Out, "none")
	}
	fmt.Fprintf(r.Out, "  Tokens: %d\n", r.ProjectCtx.TotalTokens)
	if r.IsGitRepo {
		fmt.Fprintf(r.Out, "  Git: %s branch %s\n", tui.Success("yes"), r.GitBranch)
	}
}

func (r *REPL) handleModels() {
	registry := model.NewModelRegistry()
	for _, m := range model.BuiltInCatalog() {
		_ = registry.Add(m)
	}
	manager, err := localmodel.NewManager(r.ModelDir, registry)
	if err != nil {
		fmt.Fprintf(r.ErrOut, "Failed to load models: %v\n", err)
		return
	}
	all := manager.ListAll()
	fmt.Fprintln(r.Out, tui.Primary("Models:"))
	fmt.Fprintln(r.Out, tui.Muted("LOCAL"))
	for _, m := range all {
		status := tui.Warning("not installed")
		if m.Installed {
			status = tui.Success("installed")
		}
		fmt.Fprintf(r.Out, "  %s (%s) - %s\n", m.Name, m.ID, status)
	}
	freeModels := model.FreeCloudCatalog()
	if len(freeModels) > 0 {
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, tui.Primary("FREE CLOUD"))
		for _, m := range freeModels {
			fmt.Fprintf(r.Out, "  %s (%s) - %s\n", m.Name, m.ID, tui.Success("Free"))
		}
	}
	fmt.Fprintln(r.Out)
	fmt.Fprintln(r.Out, tui.Muted("Use apcode models --free for free cloud model details."))
}

func (r *REPL) handleRuntime() {
	if r.Runtime == nil {
		fmt.Fprintln(r.Out, tui.Warning("No runtime detected"))
		return
	}
	st, _ := r.Runtime.Status(context.Background())
	fmt.Fprintf(r.Out, "%s Runtime: %s (%s)\n", tui.Primary("Runtime:"), st.Type, st.State)
	fmt.Fprintf(r.Out, "  Available: %v\n", st.Available)
	if st.ModelID != "" {
		fmt.Fprintf(r.Out, "  Model: %s\n", st.ModelID)
	}
	if r.Model != nil {
		fmt.Fprintf(r.Out, "  Local model: %s\n", r.Model.Name)
	} else {
		fmt.Fprintln(r.Out, tui.Warning("  No local model installed"))
	}
}

func (r *REPL) handleDiff(ctx context.Context) {
	tool, ok := r.Registry.Get("git_diff")
	if !ok {
		tool, ok = r.Registry.Get("GitDiff")
	}
	if !ok {
		fmt.Fprintln(r.ErrOut, "git_diff tool not available")
		return
	}
	res, err := tool.Execute(ctx, tools.Input{})
	if err != nil {
		fmt.Fprintf(r.ErrOut, "diff error: %v\n", err)
		return
	}
	if res.Err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Warning("Diff:"), res.Err)
		if res.Output != "" {
			fmt.Fprintln(r.Out, res.Output)
		}
		return
	}
	if strings.TrimSpace(res.Output) == "" {
		fmt.Fprintln(r.Out, tui.Muted("No changes."))
		return
	}
	fmt.Fprintln(r.Out, tui.Primary("Diff:"))
	fmt.Fprintln(r.Out, tui.RenderDiff(res.Output))
}

// handleStatus renders the full project/system/runtime status screen.
func (r *REPL) handleStatus() {
	lang := "unknown"
	files := 0
	if r.ProjectCtx != nil {
		files = len(r.ProjectCtx.Files)
		max := 0
		for l, c := range r.ProjectCtx.Languages {
			if c > max {
				max = c
				lang = l
			}
		}
	}

	changes := ""
	if tool, ok := r.gitTool(); ok {
		res, err := tool.Execute(context.Background(), tools.Input{})
		if err == nil && res.Err == nil {
			lines := strings.Count(strings.TrimSpace(res.Output), "\n") + 1
			if strings.TrimSpace(res.Output) == "" {
				changes = "clean"
			} else if lines == 1 {
				changes = "1 changed file"
			} else {
				changes = fmt.Sprintf("%d changed files", lines)
			}
		}
	}

	gpuName := ""
	if r.Hardware.GPU.Known {
		gpuName = r.Hardware.GPU.Name
	}

	rtState := "not detected"
	rtReady := false
	rtName := r.RuntimeName
	if rtName == "" && r.Runtime != nil {
		rtName = string(r.Runtime.Type())
	}
	switch {
	case r.Runtime == nil:
	case !runtime.IsAvailable(context.Background(), r.Runtime):
		rtState = "unavailable"
	case r.Model == nil:
		rtState = "no model installed"
	default:
		rtState = "Ready"
		rtReady = true
	}

	modelLabel := ""
	if r.Model != nil {
		modelLabel = r.Model.Name
	}

	tui.RenderStatus(r.Out, tui.StatusData{
		Version:         config.Version,
		ProjectLanguage: lang,
		ProjectFiles:    files,
		GitRepo:         r.IsGitRepo,
		GitBranch:       r.GitBranch,
		GitChanges:      changes,
		OS:              r.Hardware.OS + "/" + r.Hardware.Arch,
		CPU:             fmt.Sprintf("%d threads", r.Hardware.LogicalCPUs),
		RAM:             formatBytes(r.Hardware.TotalRAMBytes),
		GPU:             gpuName,
		RuntimeName:     rtName,
		RuntimeModel:    modelLabel,
		RuntimeState:    rtState,
		RuntimeReady:    rtReady,
	})
}

// gitTool returns the registry's git status tool, tolerating naming variants.
func (r *REPL) gitTool() (tools.Tool, bool) {
	tool, ok := r.Registry.Get("git_status")
	if !ok {
		tool, ok = r.Registry.Get("GitStatus")
	}
	return tool, ok
}

// runAgent handles normal user text via agent/runtime with loop and history.
func (r *REPL) runAgent(ctx context.Context, prompt string) (string, error) {
	if r.Runtime == nil {
		return "", fmt.Errorf("No runtime available.\n\nRun:\n  apcode runtime\n\nInstall a runtime to enable inference.")
	}
	if r.Model == nil {
		return "No local model is installed.\n\nUse:\n  apcode models\n\nA local model is required for AI inference.", nil
	}
	// Vision capability guard: do not send images to text-only models.
	if len(r.Attachments) > 0 {
		if err := r.ensureVisionModelForRequest(); err != nil {
			// Wrap with sentinel so caller can preserve attachments.
			return "", fmt.Errorf("%w: %v", vision.ErrNotVisionModel, err)
		}
	}
	if err := r.Runtime.Load(ctx, r.Model); err != nil {
		// Handle specific error states with useful messages
		var re *runtime.RuntimeError
		if errors.As(err, &re) {
			switch re.Code {
			case runtime.CodeInsufficientMemory:
				return "", fmt.Errorf("Insufficient RAM to load model %s (%s required). Try a smaller model like gemma-2b-q4 or close other applications.\nRun: apcode models", r.Model.ID, formatBytes(r.Model.MinimumRAMBytes))
			case runtime.CodeModelCorrupted:
				return "", fmt.Errorf("Model file corrupted for %s at %s. Reinstall with: apcode models install %s", r.Model.ID, r.Model.InstallPath, r.Model.ID)
			case runtime.CodeModelNotInstalled:
				return "", fmt.Errorf("Model file missing for %s at %s. Reinstall with: apcode models install %s", r.Model.ID, r.Model.InstallPath, r.Model.ID)
			case runtime.CodeIncompatibleModel:
				return "", fmt.Errorf("Model %s is incompatible with runtime %s. Try: apcode recommend", r.Model.ID, r.Runtime.Type())
			case runtime.CodeRuntimeUnavailable:
				return "", fmt.Errorf("Runtime %s unavailable. Check: apcode runtime", r.Runtime.Type())
			}
		}
		return "", fmt.Errorf("Failed to load model %s: %v", r.Model.ID, err)
	}
	defer func() { _ = r.Runtime.Unload(context.Background()) }()

	const maxIter = 10
	const maxRepairAttempts = 2
	modified := false
	repairAttempts := 0
	planEmitted := false
	// The agent system prompt. The tool catalog comes from the registry —
	// the single source of truth — so the model can only ever see tools
	// that APCode actually has.
	systemPrompt := "You are APCode, an offline AI coding agent working inside the user's project at " + r.Workspace + ".\n" +
		"Rules: inspect before modifying; use tools instead of guessing; make minimal changes that preserve existing architecture; " +
		"avoid unrelated changes; validate changes by running tests when available; never claim success without verification.\n" +
		"FILE RULES: never assume or invent file names or paths (no App.css/App.js guesses). " +
		"Only reference files you have seen in tool results. Discover real files with list_files/search first.\n" +
		r.Registry.DefinitionsForPrompt()
	for iter := 0; iter < maxIter; iter++ {
		historyStr := ""
		if len(r.History) > 0 {
			start := 0
			if len(r.History) > 10 {
				start = len(r.History) - 10
			}
			var b strings.Builder
			for i := start; i < len(r.History); i++ {
				m := r.History[i]
				b.WriteString(m.Role)
				b.WriteString(": ")
				b.WriteString(m.Content)
				b.WriteString("\n")
			}
			historyStr = b.String()
		}
		fullPrompt := prompt
		if iter > 0 {
			fullPrompt = historyStr
		} else if historyStr != "" {
			fullPrompt = "Conversation history:\n" + historyStr + "\nCurrent prompt: " + prompt
		}
		if r.ProjectCtx != nil && len(r.ProjectCtx.Files) > 0 && iter == 0 {
			fullPrompt = fmt.Sprintf("Project: %s (%d files)\n%s\n\nUser: %s", r.Workspace, len(r.ProjectCtx.Files), historyStr, prompt)
		}
		fullPrompt = systemPrompt + "\n\n" + fullPrompt
		// Include attached images for multimodal vision (encode to base64 for Ollama LLaVA etc.)
		// Vision capability is already ensured before Load; here we just encode.
		var reqImages []string
		if len(r.Attachments) > 0 {
			for _, a := range r.Attachments {
				if err := vision.ValidateImageFile(a.Path); err != nil {
					fmt.Fprintf(r.Out, "%s Image error: %v\n", tui.Warning("⚠"), err)
					continue
				}
				if b64, err := vision.EncodeImageToBase64(a.Path); err == nil {
					reqImages = append(reqImages, b64)
				} else {
					fmt.Fprintf(r.Out, "%s Failed to encode image: %v\n", tui.Warning("⚠"), err)
				}
			}
			if iter == 0 && len(reqImages) > 0 {
				fmt.Fprintf(r.Out, "%s Sending %d image(s) with prompt: %s\n", tui.Success("→"), len(reqImages), tui.AttachmentChips(r.attachedPaths()))
			}
		}
		req := runtime.GenerateRequest{Prompt: fullPrompt, Options: runtime.GenerateOptions{MaxTokens: 512}, Images: reqImages}
		var resp *runtime.GenerateResponse
		var err error
		if iter == 0 {
			sp := tui.NewSpinner(r.Out, "Thinking...", r.interactiveOut())
			sp.Ctx = ctx
			sp.Start()
			resp, err = r.Runtime.Generate(ctx, req)
			sp.Stop()
		} else {
			resp, err = r.Runtime.Generate(ctx, req)
		}
		if err != nil {
			return "", fmt.Errorf("Inference failed: %v", err)
		}
		if resp == nil {
			return "", fmt.Errorf("No response from model")
		}
		text := strings.TrimSpace(resp.Text)
		if text == "" {
			text = "(no response)"
		}
		if plan := extractPlan(text); plan != "" {
			r.lastPlan = plan
			fmt.Fprintf(r.Out, "%s\n%s\n", tui.Accent(tui.GlyphAction+" Plan"), r.lastPlan)
		}
		r.History = append(r.History, Message{Role: "assistant", Content: text})
		toolCalls, answer, _ := parseToolCalls(text)
		if len(toolCalls) == 0 {
			// A plan without tool calls is an intermediate step: acknowledge
			// it and let the agent proceed to act. Only the first plan is
			// echoed; later plan-like answers are treated as final so that
			// genuinely textual replies still terminate the loop.
			if plan := extractPlan(text); plan != "" && !planEmitted && !modified {
				planEmitted = true
				r.History = append(r.History, Message{
					Role:    "system",
					Content: "Plan acknowledged. Proceed step by step using tools.",
				})
				continue
			}
			if answer != "" {
				text = answer
			}
			// Validation loop: if files were changed in this task, run the
			// detected test command once before accepting success. On
			// failure, feed the failure back and let the model repair,
			// bounded by maxRepairAttempts.
			if modified && repairAttempts < maxRepairAttempts {
				repairAttempts++
				vsp := tui.NewSpinner(r.Out, "Running validation...", r.interactiveOut())
				vsp.Ctx = ctx
				vsp.Start()
				vres, verr := r.Registry.Execute(ctx, "run_tests", tools.Input{})
				vsp.Stop()
				if tools.IsToolError(verrOr(vres.Err, verr), tools.CodeNotFound) {
					fmt.Fprintln(r.Out, tui.Muted("  No test command detected for this project; skipping validation."))
					return text, nil
				}
				var vout string
				failed := false
				if verr != nil || (vres.Err != nil) {
					failed = true
					vout = fmt.Sprintf("validation error: %v", verrOr(vres.Err, verr))
				} else {
					vout = vres.Output
					failed = vres.Err != nil
				}
				if failed {
					fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Validation failed"))
					fmt.Fprintln(r.Out, tui.Muted("  Investigating failure..."))
					r.History = append(r.History, Message{
						Role:    "tool_result",
						Content: "Validation (run_tests) FAILED. Output:\n" + truncateForHistory(vout, 2000) + "\nInvestigate the failure, fix the code with tools, then run run_tests again.",
					})
					continue
				}
				fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivitySuccess, "Tests passed"))
			}
			return text, nil
		}
		for _, tc := range toolCalls {
			fmt.Fprintln(r.Out, tui.ToolSummary(tc.Name, tc.Input))
			// Unified approval policy: reads are automatic; file writes,
			// deletes, patches, and non-safe terminal commands require an
			// explicit [y/N] confirmation.
			if requiresUserApproval(tc.Name, tc.Input) {
				n := normalizeToolName(tc.Name)
				switch {
				case n == "runcommand" || n == "shell":
					fullCmd := strings.TrimSpace(tc.Input["command"] + " " + tc.Input["args"])
					class := tools.ClassifyCommand(fullCmd)
					label := "wants to run"
					if class == tools.ClassBlocked {
						label = "BLOCKED command (refused by security policy)"
					}
					fmt.Fprintf(r.Out, "\n%s %s:\n\n%s\n\n", tui.Warning("APCode"), label, fullCmd)
					fmt.Fprint(r.Out, "Allow? [y/N] ")
				case n == "deletefile":
					path := tc.Input["path"]
					fmt.Fprintf(r.Out, "\n%s wants to DELETE:\n\n%s\n\n", tui.Warning("APCode"), path)
					fmt.Fprint(r.Out, "Approve deletion? [y/N] ")
				default:
					path := tc.Input["path"]
					content := tc.Input["content"]
					patch := tc.Input["patch"]
					fmt.Fprintf(r.Out, "\n%s wants to modify:\n\n%s\n\n", tui.Primary("APCode"), tui.Warning(path))
					preview := content
					if preview == "" {
						preview = patch
					}
					if len(preview) > 500 {
						preview = preview[:500] + "..."
					}
					if preview != "" {
						fmt.Fprintln(r.Out, preview)
					}
					fmt.Fprint(r.Out, "\nApply this change? [y/N] ")
				}
				if r.reader == nil {
					r.reader = bufio.NewReader(r.In)
				}
				confirm, _ := r.reader.ReadString('\n')
				confirm = strings.ToLower(strings.TrimSpace(confirm))
				if confirm != "y" && confirm != "yes" {
					fmt.Fprintln(r.Out, tui.Muted("Skipped."))
					r.History = append(r.History, Message{Role: "tool_result", Content: "Tool " + tc.Name + " cancelled by user"})
					continue
				}
				if normalizeToolName(tc.Name) == "runcommand" || normalizeToolName(tc.Name) == "shell" {
					tc.Input["confirm"] = "true"
				}
			}
			// Pre-execution validation: tool exists? required params present?
			if verr := r.Registry.Validate(tc.Name, tc.Input); verr != nil {
				var payload string
				if tools.IsToolError(verr, tools.CodeNotFound) {
					payload = r.Registry.UnknownToolPayload(tc.Name)
					fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Unknown tool: "+tc.Name))
				} else {
					payload = fmt.Sprintf(`{"error":"invalid_tool_call","tool":%q,"problem":%q}`, tc.Name, verr.Error())
					fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Invalid tool call: "+verr.Error()))
				}
				r.History = append(r.History, Message{
					Role:    "tool_result",
					Content: payload + "\nRecover by selecting a real tool from available_tools and retrying.",
				})
				continue
			}
			res, err := r.Registry.Execute(ctx, tc.Name, tc.Input)
			var out string
			if err != nil {
				out = "Tool error: " + err.Error()
				fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Tool failed: "+err.Error()))
			} else if res.Err != nil {
				out = "Tool failed: " + res.Err.Error() + "\nOutput: " + res.Output
				fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivityError, "Tool failed: "+res.Err.Error()))
			} else {
				out = res.Output
				if out == "" {
					out = "(no output)"
				}
				fmt.Fprintln(r.Out, tui.ActivityLine(tui.ActivitySuccess, tc.Name+" completed"))
				if isModifyingTool(tc.Name) {
					modified = true
					if p := tc.Input["path"]; p != "" {
						fmt.Fprintln(r.Out, tui.FileChange(p))
					}
				}
			}
			preview := out
			if len(preview) > 200 {
				preview = preview[:200]
			}
			fmt.Fprintf(r.Out, "%s\n", tui.Muted("    "+preview))
			r.History = append(r.History, Message{Role: "tool_result", Content: "Tool " + tc.Name + " result: " + out})
		}
		if iter == maxIter-1 {
			return "Max iterations reached without final answer", nil
		}
	}
	return "(no response)", nil
}

// extractPlan captures a numbered plan (3+ numbered steps or a "Plan:"
// heading followed by numbered steps) from assistant output.
func extractPlan(text string) string {
	lines := strings.Split(text, "\n")
	var planLines []string
	inPlan := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		lower := strings.ToLower(t)
		if strings.HasPrefix(lower, "plan:") || strings.HasPrefix(lower, "plan :") {
			inPlan = true
			planLines = append(planLines[:0], l)
			continue
		}
		isNumbered := len(t) > 2 && (t[0] >= '1' && t[0] <= '9') && (t[1] == '.' || t[1] == ')')
		if isNumbered {
			if !inPlan {
				inPlan = true
				planLines = nil
			}
			planLines = append(planLines, l)
		} else if inPlan {
			break
		}
	}
	if len(planLines) >= 3 {
		return strings.TrimRight(strings.Join(planLines, "\n"), "\n")
	}
	return ""
}

// verrOr returns whichever error is non-nil.
func verrOr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// truncateForHistory limits a string for inclusion in conversation history.
func truncateForHistory(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}

// parseToolCalls extracts EVERY tool call from model output. Small local
// models frequently emit several fenced JSON blocks interleaved with prose,
// or concatenated objects; the old first-{-to-last-} scan failed on those
// and the agent silently executed nothing while hallucinating results.
// Strategy:
//  1. every ```...``` fenced block is parsed independently
//  2. remaining text is scanned for balanced top-level JSON objects with a
//     "tool" field (string/escape aware, so prose between them is fine)
//  3. arrays of tool calls and {"tool_calls":[...]} wrappers are honoured
func parseToolCalls(text string) ([]struct {
	Name  string
	Input map[string]string
}, string, error) {
	type call = struct {
		Name  string
		Input map[string]string
	}
	var calls []call

	appendObj := func(obj map[string]interface{}) {
		for _, extracted := range extractToolObjs(obj) {
			calls = append(calls, call{Name: extracted.name, Input: extracted.input})
		}
	}

	handleJSON := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			appendObj(obj)
			return
		}
		var arr []map[string]interface{}
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			for _, m := range arr {
				appendObj(m)
			}
		}
	}

	remaining := text
	// 1. Fenced blocks.
	for {
		i := strings.Index(remaining, "```")
		if i == -1 {
			break
		}
		j := strings.Index(remaining[i+3:], "```")
		if j == -1 {
			break
		}
		inner := remaining[i+3 : i+3+j]
		inner = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(inner), "json"))
		handleJSON(inner)
		remaining = remaining[:i] + remaining[i+3+j+3:]
	}

	// 2. Balanced JSON object scan over what's left.
	for {
		start := strings.Index(remaining, "{")
		if start == -1 {
			break
		}
		end := balancedJSONEnd(remaining[start:])
		if end == -1 {
			break
		}
		handleJSON(remaining[start : start+end+1])
		remaining = remaining[:start] + remaining[start+end+1:]
	}

	if len(calls) == 0 {
		return nil, strings.TrimSpace(text), nil
	}
	return calls, "", nil
}

type namedTool struct {
	name  string
	input map[string]string
}

// extractToolObjs pulls tool calls from one decoded JSON object, honouring
// single form, {"tool_calls":[...]}, and array entries.
func extractToolObjs(obj map[string]interface{}) []namedTool {
	var out []namedTool
	add := func(m map[string]interface{}) {
		tn, _ := m["tool"].(string)
		if tn == "" {
			if n, ok := m["name"].(string); ok {
				tn = n
			}
		}
		if tn == "" {
			return
		}
		input := make(map[string]string)
		switch raw := m["input"].(type) {
		case map[string]interface{}:
			for k, v := range raw {
				input[k] = fmt.Sprint(v)
			}
		case string:
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(raw), &parsed) == nil {
				for k, v := range parsed {
					input[k] = fmt.Sprint(v)
				}
			} else if raw != "" {
				input["value"] = raw
			}
		}
		out = append(out, namedTool{name: strings.TrimSpace(tn), input: input})
	}
	add(obj)
	if raw, ok := obj["tool_calls"].([]interface{}); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				add(m)
			}
		}
	}
	return out
}

// balancedJSONEnd returns the index of the '}' closing the leading '{',
// honouring strings and escapes; -1 when unbalanced.
func balancedJSONEnd(s string) int {
	depth := 0
	inStr := false
	esc := false
	for i, r := range s {
		switch {
		case esc:
			esc = false
		case inStr && r == '\\':
			esc = true
		case r == '"':
			inStr = !inStr
		case !inStr && r == '{':
			depth++
		case !inStr && r == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func formatBytes(bytes uint64) string {
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/mib)
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func init() {
	_ = config.Version
}

// ---- agent-facing slash commands ----

// handleFiles lists files using the registry's file listing tool.
func (r *REPL) handleFiles() {
	dir := "."
	tool, ok := r.Registry.Get("list_files")
	if !ok {
		tool, ok = r.Registry.Get("ListDirectory")
	}
	if !ok {
		fmt.Fprintln(r.ErrOut, "file listing tool not available")
		return
	}
	res, err := tool.Execute(context.Background(), tools.Input{"path": dir})
	if err != nil || res.Err != nil {
		fmt.Fprintf(r.ErrOut, "list failed: %v\n", verrOr(res.Err, err))
		return
	}
	fmt.Fprintln(r.Out, tui.Header("Files"))
	fmt.Fprintln(r.Out, res.Output)
}

// handleSearch runs a workspace search through the registry.
func (r *REPL) handleSearch(ctx context.Context, input string) {
	query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "/search"))
	if query == "" {
		fmt.Fprintln(r.Out, tui.Muted("usage: /search <query>"))
		return
	}
	tool, ok := r.Registry.Get("search")
	if !ok {
		tool, ok = r.Registry.Get("SearchFiles")
	}
	if !ok {
		fmt.Fprintln(r.ErrOut, "search tool not available")
		return
	}
	res, err := tool.Execute(ctx, tools.Input{"query": query})
	if err != nil {
		fmt.Fprintf(r.ErrOut, "search error: %v\n", err)
		return
	}
	if res.Err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Warning("Search:"), res.Err)
		return
	}
	out := res.Output
	if strings.TrimSpace(out) == "" {
		out = "(no results)"
	}
	fmt.Fprintf(r.Out, "%s %s\n", tui.Header("Search"), tui.Muted(query))
	fmt.Fprintln(r.Out, out)
}

// handleModel shows the currently selected model.
func (r *REPL) handleModel() {
	if r.Model == nil {
		fmt.Fprintln(r.Out, tui.Warning("No local model selected."))
		fmt.Fprintln(r.Out, tui.Muted("Use /models to list and `apcode models install <id>` to install."))
		return
	}
	rt := r.RuntimeName
	if rt == "" && r.Runtime != nil {
		rt = string(r.Runtime.Type())
	}
	fmt.Fprintln(r.Out, tui.Box("Model", []string{
		fmt.Sprintf("%s %s", tui.Muted("ID      :"), r.Model.ID),
		fmt.Sprintf("%s %s", tui.Muted("Name    :"), r.Model.Name),
		fmt.Sprintf("%s %s", tui.Muted("Runtime :"), rt),
	}))
}

// handlePlan shows the plan captured from the last task.
func (r *REPL) handlePlan() {
	if r.lastPlan == "" {
		fmt.Fprintln(r.Out, tui.Muted("No plan recorded yet. Plans appear when the model proposes numbered steps for a task."))
		return
	}
	fmt.Fprintln(r.Out, tui.Header("Last Plan"))
	fmt.Fprintln(r.Out, r.lastPlan)
}

// handleCompact compacts conversation history, keeping the first user
// message (the task anchor) and the most recent turns.
func (r *REPL) handleCompact() {
	const keepRecent = 6
	if len(r.History) <= keepRecent+1 {
		fmt.Fprintln(r.Out, tui.Muted(fmt.Sprintf("History is already compact (%d messages).", len(r.History))))
		return
	}
	dropped := len(r.History) - keepRecent - 1
	compacted := []Message{r.History[0]}
	compacted = append(compacted, Message{
		Role:    "system",
		Content: fmt.Sprintf("[history compacted: %d earlier messages summarized away]", dropped),
	})
	compacted = append(compacted, r.History[len(r.History)-keepRecent:]...)
	r.History = compacted
	fmt.Fprintf(r.Out, "%s\n", tui.Success(fmt.Sprintf("✓ Compacted: removed %d older messages.", dropped)))
}

// handleTools prints the exact tool catalog from the registry — the same
// definitions the model receives in its prompt.
func (r *REPL) handleTools() {
	list := r.Registry.List()
	fmt.Fprintf(r.Out, "%s %d registered tool(s)\n", tui.Header("Registered Tools"), len(list))
	for _, tl := range list {
		schema := tl.InputSchema()
		var params []string
		for name, p := range schema.Properties {
			required := ""
			for _, req := range schema.Required {
				if req == name {
					required = " (required)"
					break
				}
			}
			params = append(params, fmt.Sprintf("%s: %s%s", name, p.Type, required))
		}
		sort.Strings(params)
		fmt.Fprintf(r.Out, "\n  %s\n", tui.Primary(tl.Name()))
		fmt.Fprintf(r.Out, "  description : %s\n", tl.Description())
		if len(params) > 0 {
			fmt.Fprintf(r.Out, "  parameters  : %s\n", strings.Join(params, ", "))
		} else {
			fmt.Fprintf(r.Out, "  parameters  : (none)\n")
		}
	}
}

// handlePermissions prints the current permission policy.
func (r *REPL) handlePermissions() {
	fmt.Fprintln(r.Out, tui.Box("Permissions", []string{
		fmt.Sprintf("%s %s", tui.Muted("Read tools        :"), tui.Success("automatic")),
		fmt.Sprintf("%s %s", tui.Muted("File writes/edits :"), tui.Warning("approval required")),
		fmt.Sprintf("%s %s", tui.Muted("File deletion     :"), tui.Warning("approval required (always)")),
		fmt.Sprintf("%s %s", tui.Muted("Safe commands     :"), tui.Success("automatic (go test, git status, ...)")),
		fmt.Sprintf("%s %s", tui.Muted("Other commands    :"), tui.Warning("approval required")),
		fmt.Sprintf("%s %s", tui.Muted("Blocked commands  :"), tui.Error("never executed")),
		fmt.Sprintf("%s %s", tui.Muted("Git commit/push   :"), tui.Error("never automatic")),
	}))
}

// handleRollback reverts the last APCode change set via the journal.
func (r *REPL) handleRollback() {
	if r.Journal == nil || r.Journal.UndoCount() == 0 {
		fmt.Fprintln(r.Out, tui.Muted("Nothing to roll back — no APCode changes recorded in this session."))
		return
	}
	fmt.Fprintln(r.Out, tui.Muted("Reverting the last APCode change..."))
	restored, err := r.Journal.Undo()
	if err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Rollback failed:"), err)
		return
	}
	if len(restored) == 0 {
		fmt.Fprintln(r.Out, tui.Muted("No files were changed in that operation."))
		return
	}
	fmt.Fprintf(r.Out, "%s Restored %d file(s):\n", tui.Success("✓"), len(restored))
	for _, p := range restored {
		fmt.Fprintf(r.Out, "  %s\n", tui.Muted(p))
	}
}

// handleBackground implements /background, /bg, /theme — dynamic terminal
// background color configuration. Updates the core theme manager in tui/color.go
// which dynamically computes 48;2 escapes for all viewports/sidebars/input boxes.
// Supports hex (#RRGGBB, #abc) and curated dark presets (dark/darker/midnight
// etc.) so users can pick a darker canvas or any custom color.
func (r *REPL) handleBackground(raw string) {
	parts := strings.Fields(raw)
	if len(parts) < 2 || strings.EqualFold(parts[1], "list") || strings.EqualFold(parts[1], "help") || parts[1] == "?" {
		cur := tui.GetBackgroundColor()
		if cur == "" {
			cur = "default (terminal)"
		}
		fmt.Fprintln(r.Out, tui.Box("Background", []string{
			fmt.Sprintf("%s %s", tui.Muted("Current :"), tui.White(cur)),
			fmt.Sprintf("%s %s", tui.Muted("Usage   :"), tui.Muted("/background #RRGGBB | /background <preset> | /background default")),
			fmt.Sprintf("%s %s", tui.Muted("Example :"), tui.Muted("/background #1A1B26  /bg #0f172a  /theme midnight")),
			fmt.Sprintf("%s %s", tui.Muted("Theme   :"), tui.Muted("applies to main viewport, sidebars, and input boxes")),
		}))
		// Show curated darker presets with preview blocks
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, tui.Bold("Dark presets — pick a darker canvas or your own hex:"))
		for _, name := range tui.BackgroundPresetNames() {
			hex := tui.BackgroundPresets()[name]
			// Use a small colored block via 48;2 for preview
			rVal, gVal, bVal := hexToRGB(hex)
			esc := fmt.Sprintf("\x1b[48;2;%d;%d;%dm  \x1b[0m", rVal, gVal, bVal)
			fmt.Fprintf(r.Out, "  %s %-10s %s %s\n", esc, name, tui.Muted(hex), func() string {
				if strings.EqualFold(cur, hex) {
					return tui.Success("← current")
				}
				return ""
			}())
		}
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, tui.Muted("Tip: darker = #0A0A0A, midnight = #080A12, ink = #0D1117 — or any #RRGGBB you like."))
		fmt.Fprintln(r.Out, tui.Muted("Your choice is saved to ~/.apcode/config.json and restored on next `apcode`."))

		return
	}
	arg := parts[1]
	if err := tui.SetBackgroundColor(arg); err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Invalid color:"), err)
		fmt.Fprintln(r.Out, tui.Muted("  Use #RRGGBB or preset, e.g. /background #1A1B26, /background midnight, /background dark"))
		fmt.Fprintln(r.Out, tui.Muted("  Presets:"), tui.Muted(strings.Join(tui.BackgroundPresetNames(), ", ")))
		return
	}
	cur := tui.GetBackgroundColor()
	if cur == "" {
		cur = "default"
	}
	// Persist choice so `apcode` remembers it next launch (whole terminal background)
	_ = tui.SaveBackgroundToConfig(cur)
	fmt.Fprintf(r.Out, "%s Background set to %s\n", tui.Success("✓"), tui.White(cur))
	fmt.Fprintln(r.Out, tui.Muted("  Saved to ~/.apcode/config.json — will restore on next `apcode`."))

	// Re-render welcome to show the new fill immediately across the whole viewport
	r.printWelcome()
}

// hexToRGB is a tiny helper for preset preview blocks.
func hexToRGB(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	var rv, gv, bv uint64
	fmt.Sscanf(hex[0:2], "%x", &rv)
	fmt.Sscanf(hex[2:4], "%x", &gv)
	fmt.Sscanf(hex[4:6], "%x", &bv)
	return int(rv), int(gv), int(bv)
}

// handleDashboard renders the active-session two-column split dashboard
// (left stream + right sidebar with session meta, todos, cost, LSP) and the
// unified bottom prompt. Demonstrates responsive handling of tea.WindowSizeMsg.
func (r *REPL) handleDashboard() {
	w, h := tui.TerminalSize()
	// Build a demo dashboard from current session state; in a real bubbletea
	// model this would be called from Update on tea.WindowSizeMsg.
	layout := tui.HandleWindowSizeMsg(tui.WindowSizeMsg{Width: w, Height: h})
	// Resolve session meta from real state
	meta := tui.SessionMetadata{
		SessionID:   "local",
		TokensUsed:  0,
		TokensTotal: 0,
		CostUSD:     0,
		LSPReady:    true,
	}
	if len(r.History) > 0 {
		meta.SessionID = fmt.Sprintf("%x", len(r.History))
		meta.TokensUsed = len(strings.Join([]string{fmt.Sprint(r.History)}, "")) / 4
		meta.TokensTotal = 128000
	}
	// Build todos from lastPlan (extracted numbered steps)
	var todos []tui.TodoItem
	if r.lastPlan != "" {
		for _, line := range strings.Split(r.lastPlan, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// naive: first item in-progress, rest pending; caller can update states
			state := tui.TodoPending
			if len(todos) == 0 {
				state = tui.TodoInProgress
			}
			todos = append(todos, tui.TodoItem{Title: line, State: state})
		}
	}
	mode := r.RuntimeName
	if mode == "" && r.Runtime != nil {
		mode = string(r.Runtime.Type())
	}
	if mode == "" {
		mode = "native"
	}
	modelName := ""
	provider := ""
	if r.Model != nil {
		modelName = r.Model.Name
		provider = string(r.Model.Provider)
	}
	// Build left stream from history
	var leftContent string
	if len(r.History) > 0 {
		var b strings.Builder
		for _, m := range r.History {
			b.WriteString(tui.Info(m.Role + ": "))
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
		leftContent = b.String()
	} else {
		leftContent = tui.Muted("  No active session output — send a task to start")
	}
	_ = layout // layout already used inside RenderDashboard; keep for tea.WindowSizeMsg demo
	out := tui.RenderDashboard(tui.DashboardOptions{
		Width:       w,
		Height:      h,
		LeftContent: leftContent,
		Meta:        meta,
		Todos:       todos,
		Mode:        mode,
		ModelName:   modelName,
		Provider:    provider,
		Workspace:   r.Workspace,
		GitBranch:   r.GitBranch,
		Version:     config.Version,
	})
	fmt.Fprintln(r.Out, out)
}

// handleTodos shows the live agent todo checklist component.
func (r *REPL) handleTodos() {
	if r.lastPlan == "" {
		fmt.Fprintln(r.Out, tui.Muted("No active plan — todos will appear when the agent emits a multi-step plan."))
		// Show demo widget so the checklist UI is still visible
		demo := []tui.TodoItem{
			{Title: "Analyze project", State: tui.TodoCompleted},
			{Title: "Edit files", State: tui.TodoInProgress},
			{Title: "Run tests", State: tui.TodoPending},
		}
		fmt.Fprintln(r.Out, tui.RenderTodoList(demo, r.uiWidth()))
		return
	}
	var todos []tui.TodoItem
	for _, line := range strings.Split(r.lastPlan, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		state := tui.TodoPending
		if len(todos) == 0 {
			state = tui.TodoInProgress
		}
		todos = append(todos, tui.TodoItem{Title: line, State: state})
	}
	fmt.Fprintln(r.Out, tui.RenderTodoList(todos, r.uiWidth()))
}

// handleImageAttach handles /image and /attach slash commands for multimodal vision.
// With a path argument the file is attached directly. Without arguments the
// platform image picker is used (native Windows Open File dialog on Windows;
// the terminal line-based picker on macOS/Linux). It never sends anything to
// the model — the image is attached to the composer and sent with the next
// prompt on Enter.
func (r *REPL) handleImageAttach(raw string) {
	// Extract path after command: "/image <path>" or "/attach <path>"
	parts := strings.Fields(raw)
	if len(parts) < 2 {
		dir := r.Workspace
		if dir == "" {
			dir, _ = os.Getwd()
		}
		picked, err := newImagePicker(r).PickImage(dir)
		if err != nil {
			fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Image error:"), err)
			return
		}
		if picked != "" {
			if err := r.handleVisionAwareAttach(picked); err != nil {
				fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Image error:"), err)
				if strings.Contains(err.Error(), "unsupported") {
					fmt.Fprintln(r.Out, tui.Muted("Supported formats: PNG, JPEG, WEBP, GIF, BMP"))
				}
			}
		}
		return
	}
	// Support paths with spaces: join remaining parts and trim quotes
	path := strings.TrimSpace(strings.TrimPrefix(raw, parts[0]))
	path = strings.TrimSpace(path)
	// Remove surrounding quotes if present
	if len(path) >= 2 && ((path[0] == '"' && path[len(path)-1] == '"') || (path[0] == '\'' && path[len(path)-1] == '\'')) {
		path = path[1 : len(path)-1]
	}
	path = strings.TrimSpace(path)
	if path == "" {
		fmt.Fprintln(r.Out, tui.Warning("Image path cannot be empty"))
		return
	}
	// Validate image file (missing, unsupported format, etc.)
	if err := vision.ValidateImageFile(path); err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Image error:"), err)
		if strings.Contains(err.Error(), "unsupported") {
			fmt.Fprintln(r.Out, tui.Muted("Supported formats: PNG, JPEG, WEBP, GIF, BMP"))
		}
		return
	}
	if err := r.handleVisionAwareAttach(path); err != nil {
		fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗ Image error:"), err)
		if strings.Contains(err.Error(), "unsupported") {
			fmt.Fprintln(r.Out, tui.Muted("Supported formats: PNG, JPEG, WEBP, GIF, BMP"))
		}
	}
}

// attachImagePath validates an image, records its metadata in the composer
// attachment state, and prints the attached-chip confirmation to the user.
// It is the legacy low-level attach; vision-aware routing should use
// handleVisionAwareAttach which handles model switching and blocking.
func (r *REPL) attachImagePath(path string) error {
	if err := vision.ValidateImageFile(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	r.Attachments = append(r.Attachments, ImageAttachment{
		Path:     path,
		Filename: filepath.Base(path),
		Mime:     vision.GetMimeType(path),
		Size:     info.Size(),
	})
	// Warn if model is not vision-capable using explicit capability + heuristic.
	if r.Model != nil && !isVisionCapableModel(r.Model) {
		fmt.Fprintf(r.Out, "%s Model %q may not support vision inputs (text-only model).\n", tui.Warning("⚠"), r.Model.ID)
		fmt.Fprintln(r.Out, tui.Muted("Try a vision model such as llava, bakllava, or qwen2-vl for best results."))
	}
	fmt.Fprintf(r.Out, "%s Attached %s\n", tui.Success("✓"), tui.AttachmentChips(r.attachedPaths()))
	fmt.Fprintln(r.Out, tui.Muted("The image will be sent with your next prompt and cleared after. Use /clear to remove it."))
	return nil
}

// attachedPaths returns the list of currently attached image paths.
func (r *REPL) attachedPaths() []string {
	paths := make([]string, 0, len(r.Attachments))
	for _, a := range r.Attachments {
		if strings.TrimSpace(a.Path) != "" {
			paths = append(paths, a.Path)
		}
	}
	return paths
}

// clearAttachments removes all attached images from the composer.
func (r *REPL) clearAttachments() {
	r.Attachments = nil
}

// handleWindowSizeMsg demonstrates explicit tea.WindowSizeMsg handling for
// terminal resizing — callers in bubbletea Update would forward the msg here
// to recompute the two-column split without wrapping bugs.
func (r *REPL) handleWindowSizeMsg(msg tui.WindowSizeMsg) tui.DashboardLayout {
	// This mirrors bubbletea's pattern: func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
	// where msg is tea.WindowSizeMsg. We delegate to the tui theme/layout engine
	// which applies the new width/height to main viewport, sidebars, and input boxes.
	layout := tui.HandleWindowSizeMsg(msg)
	// Optionally store or re-render; here we just return the computed layout
	return layout
}
