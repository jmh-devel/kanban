package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/app"
	ghclient "github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/initcmd"
	"github.com/jmh-devel/kanban/internal/repo"
	"github.com/jmh-devel/kanban/internal/state"
	"github.com/jmh-devel/kanban/internal/tui"
	"github.com/jmh-devel/kanban/internal/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = ""
	dirty     = ""
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && isVersionFlag(args[0]) {
		printVersion()
		return 0
	}

	command := "print"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	} else if len(args) == 0 && isTerminal(os.Stdout) {
		command = "tui"
	}

	switch command {
	case "init":
		return runInit(args)
	case "print":
		return runPrint(args)
	case "json":
		return runJSON(args)
	case "tui":
		return runTUI(args)
	case "serve":
		return runServe(args)
	case "open":
		return runOpen(args)
	case "move":
		return runMove(args)
	case "config":
		return runConfig(args)
	case "version":
		printVersion()
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		printUsage()
		return 2
	}
}

func isVersionFlag(arg string) bool {
	return arg == "--version" || arg == "-v"
}

func printVersion() {
	fmt.Print(versionInfo())
}

func versionInfo() string {
	built := buildDate
	if built == "" {
		built = "unknown"
	}

	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "kanban %s\n", version)
	_, _ = fmt.Fprintf(&builder, "commit: %s\n", commit)
	_, _ = fmt.Fprintf(&builder, "built: %s\n", built)
	_, _ = fmt.Fprintf(&builder, "go: %s\n", runtime.Version())
	_, _ = fmt.Fprintf(&builder, "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if dirty != "" {
		_, _ = fmt.Fprintf(&builder, "dirty: %s\n", dirty)
	}
	return builder.String()
}

func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing config command")
		printConfigUsage()
		return 2
	}
	command := args[0]
	args = args[1:]

	switch command {
	case "set-repo-key":
		return runConfigSetRepoKey(args)
	case "set-runner":
		return runConfigSetRunner(args)
	case "runners":
		return runConfigRunners(args)
	case "show":
		return runConfigShow(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown config command %q\n\n", command)
		printConfigUsage()
		return 2
	}
}

func runConfigSetRepoKey(args []string) int {
	fs := flag.NewFlagSet("config set-repo-key", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("path", ".", "path inside the target git repository")
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoKey := fs.String("repo-key", "", "tsctl repo key")
	reposFile := fs.String("repos-file", "", "tsctl repos.yaml path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*repoKey) == "" {
		fmt.Fprintln(os.Stderr, "--repo-key is required")
		return 2
	}
	details, err := repo.Detect(context.Background(), *repoPath, *repoSlug)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	config, err := state.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repoConfig := config.Repos[details.Slug]
	repoConfig.RepoKey = *repoKey
	repoConfig.ReposFile = *reposFile
	config.Repos[details.Slug] = repoConfig
	if err := state.SaveConfig(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("set repo key for %s to %s\n", details.Slug, *repoKey)
	return 0
}

func runConfigSetRunner(args []string) int {
	fs := flag.NewFlagSet("config set-runner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("path", ".", "path inside the target git repository")
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	runner := fs.String("runner", "", "preferred runner")
	mode := fs.String("mode", state.DefaultMode, "preferred mode")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*runner) == "" {
		fmt.Fprintln(os.Stderr, "--runner is required")
		return 2
	}
	details, err := repo.Detect(context.Background(), *repoPath, *repoSlug)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	config, err := state.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repoConfig := config.Repos[details.Slug]
	repoConfig.PreferredRunner = *runner
	repoConfig.PreferredMode = *mode
	config.Repos[details.Slug] = repoConfig
	if err := state.SaveConfig(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("set preferred runner for %s to %s (%s)\n", details.Slug, *runner, *mode)
	return 0
}

func runConfigRunners(args []string) int {
	fs := flag.NewFlagSet("config runners", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	config, err := state.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, name := range config.RunnerNames() {
		runner := config.Runner(name)
		kind := runner.Kind
		if kind == "" {
			kind = "local_cli"
		}
		fmt.Printf("%s\t%s\n", name, kind)
	}
	return 0
}

func runConfigShow(args []string) int {
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	config, err := state.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	addr := fs.String("addr", "", "reserved for compatibility with serve")
	_ = addr
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var details ghclient.Board
	loader := func(ctx context.Context) (ghclient.Board, error) {
		board, err := loadBoard(ctx, *repoPath, *repoSlug, ghclient.BoardOptions{})
		if err == nil {
			details = board
		}
		return board, err
	}
	client := ghclient.NewClient()
	err := tui.Run(context.Background(), tui.Options{
		Loader: loader,
		Mover: func(ctx context.Context, issue ghclient.Issue, lane string) error {
			slug := details.Repo.Slug
			if slug == "" {
				board, err := loadBoard(ctx, *repoPath, *repoSlug, ghclient.BoardOptions{})
				if err != nil {
					return err
				}
				slug = board.Repo.Slug
			}
			parsedLane, err := parseLane(lane)
			if err != nil {
				return err
			}
			return client.MoveIssue(ctx, slug, issue.Number, parsedLane)
		},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runPrint(args []string) int {
	board, err := loadBoardFromFlags("print", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(app.RenderBoardText(board))
	return 0
}

func runJSON(args []string) int {
	board, err := loadBoardFromFlags("json", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(board); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	addr := fs.String("addr", "127.0.0.1:3584", "HTTP bind address")
	doneWindowDays := fs.Int("done-window-days", ghclient.DefaultDoneDays, "days of closed issues to show in Done")
	groupByMilestone := fs.Bool("group-by-milestone", false, "preserve milestone grouping metadata within lanes")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ensureLabels(context.Background(), *repoPath, *repoSlug)

	server, err := web.NewServer(func(ctx context.Context) (ghclient.Board, error) {
		return loadBoard(ctx, *repoPath, *repoSlug, ghclient.BoardOptions{
			DoneWindowDays:   *doneWindowDays,
			GroupByMilestone: *groupByMilestone,
		})
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := server.Serve(*addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runOpen(args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	doneWindowDays := fs.Int("done-window-days", ghclient.DefaultDoneDays, "days of closed issues to show in Done")
	groupByMilestone := fs.Bool("group-by-milestone", false, "preserve milestone grouping metadata within lanes")
	idleTimeout := fs.Duration("idle-timeout", 10*time.Second, "how long after browser close/idle to stop standalone server")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ensureLabels(context.Background(), *repoPath, *repoSlug)

	addr, err := findFreeAddr()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	session := newStandaloneSessionID()
	url := "http://" + addr

	server, err := web.NewServer(func(ctx context.Context) (ghclient.Board, error) {
		return loadBoard(ctx, *repoPath, *repoSlug, ghclient.BoardOptions{
			DoneWindowDays:   *doneWindowDays,
			GroupByMilestone: *groupByMilestone,
		})
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeStandalone(addr, session, *idleTimeout)
	}()

	if err := waitForHealth(url, 3*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "standalone server did not start: %v\n", err)
		return 1
	}

	if err := openSystemBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "failed to open browser: %v\n", err)
		fmt.Fprintf(os.Stderr, "open manually: %s\n", url)
		_ = notifyStandaloneClose(url, session)
		<-errCh
		return 1
	}

	fmt.Fprintf(os.Stdout, "Opened %s\n", url)
	if err := <-errCh; err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("path", ".", "path to the local git repository")
	owner := fs.String("owner", "", "GitHub owner or organization (auto-detected from remote if available)")
	name := fs.String("name", "", "GitHub repository name (defaults to local directory name)")
	remote := fs.String("remote", "origin", "git remote name to create")
	visibility := fs.String("visibility", "public", "repository visibility: public|private")
	apply := fs.Bool("apply", false, "execute publish command (default is dry-run)")
	setupLabels := fs.Bool("setup-labels", false, "create kanban lane labels in the target repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	err := initcmd.Run(context.Background(), initcmd.Options{
		Path:        *repoPath,
		Owner:       *owner,
		Name:        *name,
		Remote:      *remote,
		Visibility:  *visibility,
		Apply:       *apply,
		SetupLabels: *setupLabels,
		Stdout:      os.Stdout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runMove(args []string) int {
	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: kanban move [--path DIR] [--repo owner/repo] ISSUE LANE")
		return 2
	}
	number, err := strconv.Atoi(fs.Arg(0))
	if err != nil || number <= 0 {
		fmt.Fprintf(os.Stderr, "invalid issue number %q\n", fs.Arg(0))
		return 2
	}
	lane, err := parseLane(fs.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	details, err := repo.Detect(ctx, *repoPath, *repoSlug)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	client := ghclient.NewClient()
	if err := client.MoveIssue(ctx, details.Slug, number, lane); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Moved #%d to %s.\n", number, laneTitle(lane))
	return 0
}

func parseLane(input string) (ghclient.Lane, error) {
	lane := strings.ToLower(strings.TrimSpace(input))
	lane = strings.ReplaceAll(lane, "_", "-")
	lane = strings.ReplaceAll(lane, " ", "-")
	switch lane {
	case "backlog":
		return ghclient.LaneBacklog, nil
	case "in-progress", "progress", "inprogress":
		return ghclient.LaneInProgress, nil
	case "review":
		return ghclient.LaneReview, nil
	case "done":
		return ghclient.LaneDone, nil
	default:
		return "", fmt.Errorf("unknown lane %q, expected backlog, in-progress, review, or done", input)
	}
}

func laneTitle(lane ghclient.Lane) string {
	switch lane {
	case ghclient.LaneBacklog:
		return "Backlog"
	case ghclient.LaneInProgress:
		return "In Progress"
	case ghclient.LaneReview:
		return "Review"
	case ghclient.LaneDone:
		return "Done"
	default:
		return string(lane)
	}
}

func loadBoardFromFlags(name string, args []string) (ghclient.Board, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	doneWindowDays := fs.Int("done-window-days", ghclient.DefaultDoneDays, "days of closed issues to show in Done")
	groupByMilestone := fs.Bool("group-by-milestone", false, "preserve milestone grouping metadata within lanes")
	if err := fs.Parse(args); err != nil {
		return ghclient.Board{}, err
	}
	return loadBoard(context.Background(), *repoPath, *repoSlug, ghclient.BoardOptions{
		DoneWindowDays:   *doneWindowDays,
		GroupByMilestone: *groupByMilestone,
	})
}

func loadBoard(ctx context.Context, startPath string, repoSlug string, options ghclient.BoardOptions) (ghclient.Board, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	details, err := repo.Detect(ctx, startPath, repoSlug)
	if err != nil {
		return ghclient.Board{}, err
	}
	client := ghclient.NewClient()
	return client.LoadBoardWithOptions(ctx, details, options)
}

// ensureLabels runs a pre-flight check before the web server starts: it
// detects the current repo slug and creates any missing kanban lane labels.
// Failures are non-fatal — a warning is printed and the server still starts.
func ensureLabels(ctx context.Context, startPath, repoSlug string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	details, err := repo.Detect(ctx, startPath, repoSlug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kanban: label pre-flight: could not detect repo: %v\n", err)
		return
	}
	fmt.Printf("kanban: checking lane labels for %s...\n", details.Slug)
	if err := initcmd.EnsureLaneLabels(ctx, details.Slug, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "kanban: label pre-flight warning: %v\n", err)
	}
}

func printUsage() {
	fmt.Println(`kanban: lightweight GitHub board CLI

Usage:
	kanban init [--path DIR] [--owner ORG] [--name REPO] [--remote NAME] [--visibility public|private] [--apply] [--setup-labels]
	kanban tui [--path DIR] [--repo owner/repo] [--addr HOST:PORT]
	kanban open [--path DIR] [--repo owner/repo] [--idle-timeout 10s]
  kanban [print] [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban json [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban serve [--addr HOST:PORT] [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban move [--path DIR] [--repo owner/repo] ISSUE backlog|in-progress|review|done
  kanban config set-repo-key --repo-key NAME [--repos-file PATH] [--path DIR] [--repo owner/repo]
  kanban config set-runner --runner NAME [--mode implement|plan|review|audit] [--path DIR] [--repo owner/repo]
  kanban config runners
  kanban config show
  kanban version

Behavior:
	- init is dry-run by default and prints the gh publish command it would run
	- init --setup-labels creates kanban:in-progress and kanban:review when missing
	- init derives --owner from the existing git remote when available
	- open starts a temporary local web UI, launches your browser, and exits when the window closes
  - defaults to the git repository under the current working directory
  - resolves the GitHub slug from remote.origin.url unless --repo is provided
  - uses GitHub labels as the source of truth for board lanes`)
}

func findFreeAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("find free port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

func newStandaloneSessionID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func waitForHealth(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("health check timeout after %s", timeout)
}

func notifyStandaloneClose(baseURL string, session string) error {
	body := strings.NewReader(fmt.Sprintf(`{"session":"%s"}`, session))
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/standalone/close", body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func openSystemBrowser(url string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{url}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		command = "xdg-open"
		args = []string{url}
	}
	return exec.Command(command, args...).Start()
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func printConfigUsage() {
	fmt.Println(`Usage:
  kanban config set-repo-key --repo-key NAME [--repos-file PATH] [--path DIR] [--repo owner/repo]
  kanban config set-runner --runner NAME [--mode implement|plan|review|audit] [--path DIR] [--repo owner/repo]
  kanban config runners
  kanban config show`)
}
