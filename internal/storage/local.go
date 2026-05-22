package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	Reader   io.ReadCloser
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
	relativeName, err := LocalRelativePath(artifact.Url)
	if err != nil {
		return ArtifactContent{}, err
	}
	root, err := s.openRoot()
	if err != nil {
		return ArtifactContent{}, err
	}
	defer root.Close()
	relativePath := filepath.FromSlash(relativeName)
	info, err := root.Stat(relativePath)
	if err != nil {
		return ArtifactContent{}, fmt.Errorf("stat local artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ArtifactContent{}, errors.New("local artifact must be a regular file")
	}
	file, err := root.OpenFile(relativePath, os.O_RDONLY, 0)
	if err != nil {
		return ArtifactContent{}, fmt.Errorf("open local artifact: %w", err)
	}
	name := strings.TrimSpace(artifact.Name)
	if name == "" {
		name = filepath.Base(relativePath)
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
		Size:     info.Size(),
		Reader:   file,
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
	if !info.Mode().IsRegular() {
		return StoredArtifact{}, errors.New("source artifact must be a regular file")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("open source artifact: %w", err)
	}
	defer source.Close()
	name, err := cleanArtifactName(firstNonEmpty(preferredName, filepath.Base(sourcePath)))
	if err != nil {
		return StoredArtifact{}, err
	}
	token, err := randomPathToken()
	if err != nil {
		return StoredArtifact{}, err
	}
	relativeName := uniqueArtifactPath(name, token)
	rootPath := strings.TrimSpace(s.root)
	if rootPath == "" {
		return StoredArtifact{}, errors.New("artifact root is not configured")
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return StoredArtifact{}, fmt.Errorf("create artifact root: %w", err)
	}
	root, err := s.openRoot()
	if err != nil {
		return StoredArtifact{}, err
	}
	defer root.Close()
	relativePath := filepath.FromSlash(relativeName)
	if dir := filepath.Dir(relativePath); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return StoredArtifact{}, fmt.Errorf("create artifact directory: %w", err)
		}
	}
	file, err := root.OpenFile(relativePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("write local artifact: %w", err)
	}
	written, err := io.Copy(file, source)
	if err != nil {
		_ = file.Close()
		return StoredArtifact{}, fmt.Errorf("write local artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return StoredArtifact{}, fmt.Errorf("write local artifact: %w", err)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return StoredArtifact{
		Name:     name,
		URL:      localArtifactURL(relativeName),
		MimeType: mimeType,
		Size:     written,
	}, nil
}

func (s *LocalStore) Remove(_ context.Context, rawURL string) error {
	relativeName, err := LocalRelativePath(rawURL)
	if err != nil {
		return err
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(filepath.FromSlash(relativeName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove local artifact: %w", err)
	}
	return nil
}

func (s *LocalStore) openRoot() (*os.Root, error) {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return nil, errors.New("artifact root is not configured")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open artifact root: %w", err)
	}
	return opened, nil
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

func localArtifactURL(relativeName string) string {
	artifactURL := url.URL{
		Scheme: "local",
		Host:   "artifacts",
		Path:   "/" + strings.ReplaceAll(relativeName, "\\", "/"),
	}
	return artifactURL.String()
}

func randomPathToken() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate artifact path token: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func uniqueArtifactPath(name string, token string) string {
	name = path.Clean(strings.ReplaceAll(name, string(filepath.Separator), "/"))
	dir := path.Dir(name)
	base := path.Base(name)
	uniqueBase := token + "-" + base
	if dir == "." || dir == "/" {
		return uniqueBase
	}
	return path.Join(dir, uniqueBase)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
