package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
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
	mux.HandleFunc("GET /healthz", s.handleHealth)

	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("kanban server listening on http://%s", addr)
	return server.ListenAndServe()
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

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, strings.TrimSpace(time.Since(start).String()))
	})
}
