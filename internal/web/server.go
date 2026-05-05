package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/agent"
	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/state"
)

//go:embed templates/index.html
var templateFS embed.FS

type Loader func(context.Context) (github.Board, error)

type Server struct {
	loader Loader
	tmpl   *template.Template
}

func NewServer(loader Loader) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return &Server{loader: loader, tmpl: tmpl}, nil
}

func (s *Server) Serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/board", s.handleBoard)
	mux.HandleFunc("GET /api/dispatch/options", s.handleDispatchOptions)
	mux.HandleFunc("POST /api/dispatch", s.handleDispatch)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("kanban server listening on http://%s", addr)
	return server.ListenAndServe()
}

func (s *Server) handleDispatchOptions(w http.ResponseWriter, r *http.Request) {
	board, err := s.loader(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	config, err := state.LoadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	repoConfig := config.Repos[board.Repo.Slug]
	runner := repoConfig.PreferredRunner
	if runner == "" {
		runner = state.DefaultRunner
	}
	if requestedRunner := strings.TrimSpace(r.URL.Query().Get("runner")); requestedRunner != "" {
		runner = requestedRunner
	}
	mode := repoConfig.PreferredMode
	if mode == "" {
		mode = state.DefaultMode
	}
	if requestedMode := strings.TrimSpace(r.URL.Query().Get("mode")); requestedMode != "" {
		mode = requestedMode
	}
	issue, _ := strconv.Atoi(r.URL.Query().Get("issue"))
	preview, previewErr := agent.Preview(config, board.Repo.Slug, issue, runner, mode)
	response := map[string]any{
		"repo":       board.Repo.Slug,
		"repo_key":   repoConfig.RepoKey,
		"repos_file": repoConfig.ReposFile,
		"runner":     runner,
		"mode":       mode,
		"runners":    config.RunnerNames(),
		"modes":      []string{"implement", "plan", "review", "audit"},
		"preview":    preview,
	}
	if previewErr != nil {
		response["error"] = previewErr.Error()
	}
	writeJSON(w, response)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Issue            int    `json:"issue"`
		Runner           string `json:"runner"`
		Mode             string `json:"mode"`
		ConfirmDuplicate bool   `json:"confirm_duplicate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	board, err := s.loader(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	config, err := state.LoadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dispatcher := agent.NewDispatcher()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := dispatcher.Dispatch(ctx, config, agent.Request{
		Repo:             board.Repo.Slug,
		Issue:            request.Issue,
		Runner:           request.Runner,
		Mode:             request.Mode,
		ConfirmDuplicate: request.ConfirmDuplicate,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	board, err := s.loader(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", board); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	board, err := s.loader(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(board)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, strings.TrimSpace(time.Since(start).String()))
	})
}
