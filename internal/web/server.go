package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmh-devel/kanban/internal/agent"
	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/state"
)

//go:embed templates/index.html
var templateFS embed.FS

type Loader func(context.Context) (github.Board, error)

type Server struct {
	loader     Loader
	tmpl       *template.Template
	standalone *standaloneTracker
}

type standaloneTracker struct {
	session   string
	timeout   time.Duration
	lastSeen  time.Time
	hasSeen   bool
	requested bool
	mu        sync.Mutex
}

func newStandaloneTracker(session string, timeout time.Duration) *standaloneTracker {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &standaloneTracker{session: session, timeout: timeout}
}

func (s *standaloneTracker) Touch(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session != s.session {
		return false
	}
	s.lastSeen = time.Now().UTC()
	s.hasSeen = true
	return true
}

func (s *standaloneTracker) Close(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session != s.session {
		return false
	}
	s.requested = true
	return true
}

func (s *standaloneTracker) ShouldShutdown(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requested {
		return true
	}
	if !s.hasSeen {
		return false
	}
	return now.Sub(s.lastSeen) > s.timeout
}

func NewServer(loader Loader) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return &Server{loader: loader, tmpl: tmpl}, nil
}

func (s *Server) Serve(addr string) error {
	s.standalone = nil
	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(s.newMux()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("kanban server listening on http://%s", addr)
	return server.ListenAndServe()
}

func (s *Server) ServeStandalone(addr string, session string, idleTimeout time.Duration) error {
	if strings.TrimSpace(session) == "" {
		return errors.New("standalone session is required")
	}
	s.standalone = newStandaloneTracker(session, idleTimeout)

	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(s.newMux()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	watchDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-ticker.C:
				if !s.standalone.ShouldShutdown(time.Now().UTC()) {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = server.Shutdown(ctx)
				cancel()
				return
			}
		}
	}()

	log.Printf("kanban standalone server listening on http://%s", addr)
	err := server.ListenAndServe()
	close(watchDone)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/board", s.handleBoard)
	mux.HandleFunc("GET /api/dispatch/options", s.handleDispatchOptions)
	mux.HandleFunc("POST /api/dispatch", s.handleDispatch)
	mux.HandleFunc("POST /api/standalone/heartbeat", s.handleStandaloneHeartbeat)
	mux.HandleFunc("POST /api/standalone/close", s.handleStandaloneClose)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return mux
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
	view := struct {
		github.Board
		StandaloneSession string
	}{
		Board: board,
	}
	if s.standalone != nil {
		view.StandaloneSession = s.standalone.session
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleStandaloneHeartbeat(w http.ResponseWriter, r *http.Request) {
	if s.standalone == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	session := decodeStandaloneSession(r)
	if !s.standalone.Touch(session) {
		http.Error(w, "invalid standalone session", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStandaloneClose(w http.ResponseWriter, r *http.Request) {
	if s.standalone == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	session := decodeStandaloneSession(r)
	if !s.standalone.Close(session) {
		http.Error(w, "invalid standalone session", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeStandaloneSession(r *http.Request) string {
	if session := strings.TrimSpace(r.URL.Query().Get("session")); session != "" {
		return session
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err == nil && len(body) > 0 {
		var payload struct {
			Session string `json:"session"`
		}
		if json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Session) != "" {
			return strings.TrimSpace(payload.Session)
		}
		raw := strings.TrimSpace(string(body))
		raw = strings.Trim(raw, "\"")
		if raw != "" && !strings.Contains(raw, "=") && !strings.Contains(raw, "{") {
			return raw
		}
		if values, parseErr := parseQuery(raw); parseErr == nil {
			if session := strings.TrimSpace(values.Get("session")); session != "" {
				return session
			}
		}
	}
	_ = r.ParseForm()
	return strings.TrimSpace(r.FormValue("session"))
}

func parseQuery(raw string) (mapValues, error) {
	pairs := strings.Split(raw, "&")
	values := mapValues{}
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		key := parts[0]
		if key == "" {
			return nil, errors.New("invalid query")
		}
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		values[key] = append(values[key], value)
	}
	return values, nil
}

type mapValues map[string][]string

func (m mapValues) Get(key string) string {
	values := m[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
