package storage

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/vivek/agent-task-tracker/internal/db"
)

const localArtifactPrefix = "local://artifacts/"

type LocalStore struct {
	root string
}

type ArtifactContent struct {
	Name     string
	MimeType string
	Size     int64
	Data     []byte
}

type StoredArtifact struct {
	Name     string
	URL      string
	MimeType string
	Size     int64
}

func NewLocalStore(root string) *LocalStore {
	return &LocalStore{root: root}
}

func (s *LocalStore) Open(_ context.Context, artifact db.Artifact) (ArtifactContent, error) {
	fullPath, err := s.resolve(artifact.Url)
	if err != nil {
		return ArtifactContent{}, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ArtifactContent{}, fmt.Errorf("read local artifact: %w", err)
	}
	name := strings.TrimSpace(artifact.Name)
	if name == "" {
		name = filepath.Base(fullPath)
	}
	mimeType := strings.TrimSpace(artifact.MimeType)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return ArtifactContent{
		Name:     name,
		MimeType: mimeType,
		Size:     int64(len(data)),
		Data:     data,
	}, nil
}

func (s *LocalStore) StoreFile(_ context.Context, sourcePath string, preferredName string) (StoredArtifact, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return StoredArtifact{}, errors.New("source path is required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("stat source artifact: %w", err)
	}
	if info.IsDir() {
		return StoredArtifact{}, errors.New("source artifact must be a file")
	}
	name, err := cleanArtifactName(firstNonEmpty(preferredName, filepath.Base(sourcePath)))
	if err != nil {
		return StoredArtifact{}, err
	}
	destination, err := s.resolve(localArtifactPrefix + path.Clean(strings.ReplaceAll(name, string(filepath.Separator), "/")))
	if err != nil {
		return StoredArtifact{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return StoredArtifact{}, fmt.Errorf("create artifact directory: %w", err)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("read source artifact: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return StoredArtifact{}, fmt.Errorf("write local artifact: %w", err)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return StoredArtifact{
		Name:     name,
		URL:      localArtifactPrefix + strings.ReplaceAll(name, string(filepath.Separator), "/"),
		MimeType: mimeType,
		Size:     int64(len(data)),
	}, nil
}

func (s *LocalStore) resolve(rawURL string) (string, error) {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "", errors.New("artifact root is not configured")
	}
	relative, err := LocalRelativePath(rawURL)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func LocalRelativePath(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid local artifact URL: %w", err)
	}
	if parsed.Scheme != "local" || parsed.Host != "artifacts" {
		return "", fmt.Errorf("invalid local artifact URL %q", rawURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid local artifact URL %q", rawURL)
	}
	cleaned, err := cleanArtifactName(strings.TrimPrefix(parsed.Path, "/"))
	if err != nil {
		return "", err
	}
	return cleaned, nil
}

func IsLocalArtifactURL(rawURL string) bool {
	_, err := LocalRelativePath(rawURL)
	return err == nil
}

func cleanArtifactName(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return "", errors.New("invalid local artifact URL: path is required")
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return "", errors.New("invalid local artifact URL: path escapes artifact root")
		}
	}
	cleaned := path.Clean("/" + name)
	if cleaned == "/" || strings.HasPrefix(cleaned, "/../") || cleaned == "/.." {
		return "", errors.New("invalid local artifact URL: path escapes artifact root")
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid local artifact URL: path escapes artifact root")
	}
	return cleaned, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
