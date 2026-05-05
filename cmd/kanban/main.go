package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/app"
	ghclient "github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/initcmd"
	"github.com/jmh-devel/kanban/internal/repo"
	"github.com/jmh-devel/kanban/internal/state"
	"github.com/jmh-devel/kanban/internal/web"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "print"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "init":
		return runInit(args)
	case "print":
		return runPrint(args)
	case "json":
		return runJSON(args)
	case "serve":
		return runServe(args)
	case "config":
		return runConfig(args)
	case "version", "--version", "-v":
		fmt.Println(version)
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
	if err := fs.Parse(args); err != nil {
		return 2
	}

	server, err := web.NewServer(func(ctx context.Context) (ghclient.Board, error) {
		return loadBoard(ctx, *repoPath, *repoSlug)
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

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("path", ".", "path to the local git repository")
	owner := fs.String("owner", "", "GitHub owner or organization (auto-detected from remote if available)")
	name := fs.String("name", "", "GitHub repository name (defaults to local directory name)")
	remote := fs.String("remote", "origin", "git remote name to create")
	visibility := fs.String("visibility", "public", "repository visibility: public|private")
	apply := fs.Bool("apply", false, "execute publish command (default is dry-run)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	err := initcmd.Run(context.Background(), initcmd.Options{
		Path:       *repoPath,
		Owner:      *owner,
		Name:       *name,
		Remote:     *remote,
		Visibility: *visibility,
		Apply:      *apply,
		Stdout:     os.Stdout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func loadBoardFromFlags(name string, args []string) (ghclient.Board, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	if err := fs.Parse(args); err != nil {
		return ghclient.Board{}, err
	}
	return loadBoard(context.Background(), *repoPath, *repoSlug)
}

func loadBoard(ctx context.Context, startPath string, repoSlug string) (ghclient.Board, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	details, err := repo.Detect(ctx, startPath, repoSlug)
	if err != nil {
		return ghclient.Board{}, err
	}
	client := ghclient.NewClient()
	return client.LoadBoard(ctx, details)
}

func printUsage() {
	fmt.Println(`kanban: lightweight GitHub board CLI

Usage:
	kanban init [--path DIR] [--owner ORG] [--name REPO] [--remote NAME] [--visibility public|private] [--apply]
  kanban [print] [--path DIR] [--repo owner/repo]
  kanban json [--path DIR] [--repo owner/repo]
  kanban serve [--addr HOST:PORT] [--path DIR] [--repo owner/repo]
  kanban config set-repo-key --repo-key NAME [--repos-file PATH] [--path DIR] [--repo owner/repo]
  kanban config set-runner --runner NAME [--mode implement|plan|review|audit] [--path DIR] [--repo owner/repo]
  kanban config runners
  kanban config show
  kanban version

Behavior:
	- init is dry-run by default and prints the gh publish command it would run
	- init derives --owner from the existing git remote when available
  - defaults to the git repository under the current working directory
  - resolves the GitHub slug from remote.origin.url unless --repo is provided
  - uses the GitHub CLI for milestone and issue data`)
}

func printConfigUsage() {
	fmt.Println(`Usage:
  kanban config set-repo-key --repo-key NAME [--repos-file PATH] [--path DIR] [--repo owner/repo]
  kanban config set-runner --runner NAME [--mode implement|plan|review|audit] [--path DIR] [--repo owner/repo]
  kanban config runners
  kanban config show`)
}
