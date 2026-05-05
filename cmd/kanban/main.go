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
	doneWindowDays := fs.Int("done-window-days", ghclient.DefaultDoneWindowDays, "number of days of closed issues to show in Done")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	server, err := web.NewServer(func(ctx context.Context) (ghclient.Board, error) {
		return loadBoard(ctx, *repoPath, *repoSlug, *doneWindowDays)
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
	setupLabels := fs.Bool("setup-labels", false, "create required kanban lane labels in the GitHub repo")
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

func loadBoardFromFlags(name string, args []string) (ghclient.Board, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoSlug := fs.String("repo", "", "explicit GitHub repo slug (owner/repo)")
	repoPath := fs.String("path", ".", "path inside the target git repository")
	doneWindowDays := fs.Int("done-window-days", ghclient.DefaultDoneWindowDays, "number of days of closed issues to show in Done")
	if err := fs.Parse(args); err != nil {
		return ghclient.Board{}, err
	}
	return loadBoard(context.Background(), *repoPath, *repoSlug, *doneWindowDays)
}

func loadBoard(ctx context.Context, startPath string, repoSlug string, doneWindowDays int) (ghclient.Board, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	details, err := repo.Detect(ctx, startPath, repoSlug)
	if err != nil {
		return ghclient.Board{}, err
	}
	client := ghclient.NewClient()
	return client.LoadBoardWithOptions(ctx, details, ghclient.LoadOptions{DoneWindowDays: doneWindowDays})
}

func printUsage() {
	fmt.Println(`kanban: lightweight GitHub board CLI

Usage:
	kanban init [--path DIR] [--owner ORG] [--name REPO] [--remote NAME] [--visibility public|private] [--apply] [--setup-labels]
  kanban [print] [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban json [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban serve [--addr HOST:PORT] [--path DIR] [--repo owner/repo] [--done-window-days N]
  kanban version

Behavior:
	- init is dry-run by default and prints the gh publish command it would run
	- init derives --owner from the existing git remote when available
  - defaults to the git repository under the current working directory
  - resolves the GitHub slug from remote.origin.url unless --repo is provided
  - uses the GitHub CLI for milestone and issue data`)
}
