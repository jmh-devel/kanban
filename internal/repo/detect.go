package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

type Details struct {
	RootPath  string `json:"root_path"`
	RemoteURL string `json:"remote_url"`
	WebURL    string `json:"web_url"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
}

func Detect(ctx context.Context, startPath string, explicitRepo string) (Details, error) {
	rootPath, err := gitRoot(ctx, startPath)
	if err != nil {
		if explicitRepo == "" {
			return Details{}, err
		}
		rootPath = filepath.Clean(startPath)
	}

	if explicitRepo != "" {
		details, err := ParseSlug(explicitRepo)
		if err != nil {
			return Details{}, err
		}
		details.RootPath = rootPath
		return details, nil
	}

	remoteURL, err := gitOriginRemote(ctx, rootPath)
	if err != nil {
		return Details{}, err
	}

	details, err := ParseRemoteURL(remoteURL)
	if err != nil {
		return Details{}, err
	}
	details.RootPath = rootPath
	details.RemoteURL = remoteURL
	return details, nil
}

func ParseSlug(slug string) (Details, error) {
	parts := strings.Split(strings.TrimSpace(slug), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Details{}, fmt.Errorf("invalid repo slug %q, expected owner/repo", slug)
	}
	return Details{
		Owner:  parts[0],
		Name:   parts[1],
		Slug:   parts[0] + "/" + parts[1],
		WebURL: "https://github.com/" + parts[0] + "/" + parts[1],
	}, nil
}

func ParseRemoteURL(remote string) (Details, error) {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if remote == "" {
		return Details{}, errors.New("empty git remote URL")
	}

	if strings.HasPrefix(remote, "git@github.com:") {
		return ParseSlug(strings.TrimPrefix(remote, "git@github.com:"))
	}

	if strings.HasPrefix(remote, "ssh://") || strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://") {
		u, err := url.Parse(remote)
		if err != nil {
			return Details{}, fmt.Errorf("parse remote URL: %w", err)
		}
		if !strings.EqualFold(u.Hostname(), "github.com") {
			return Details{}, fmt.Errorf("remote host %q is not github.com", u.Hostname())
		}
		return ParseSlug(strings.TrimPrefix(u.Path, "/"))
	}

	return Details{}, fmt.Errorf("unsupported remote URL %q", remote)
}

func gitRoot(ctx context.Context, startPath string) (string, error) {
	out, err := runGit(ctx, startPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("detect git root: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func gitOriginRemote(ctx context.Context, repoPath string) (string, error) {
	out, err := runGit(ctx, repoPath, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("read origin remote: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func runGit(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return string(out), nil
}
