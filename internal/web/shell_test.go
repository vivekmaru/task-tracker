package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScopedNavigationPreservesScopeAndMarksActiveRoute(t *testing.T) {
	items := scopedNavigationItems(pageContext{ActiveRoute: "tickets", WorkspaceID: "workspace", ProjectID: "project"})
	if len(items) != 6 {
		t.Fatalf("expected six navigation items, got %d", len(items))
	}
	var active bool
	for _, item := range items {
		if item.Label != "Workspaces" && !strings.Contains(item.Href, "workspace_id=workspace") {
			t.Fatalf("expected scoped nav href, got %#v", item)
		}
		if item.Label == "Tickets" {
			active = item.Active
		}
	}
	if !active {
		t.Fatal("expected tickets navigation to be active")
	}
}

func TestLocalHTMXAssetHasImmutableCaching(t *testing.T) {
	handler := NewHandler(nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/htmx-2.0.4.min.js", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected asset response: %d %#v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Body.String(), "2.0.4") {
		t.Fatalf("expected pinned asset version, got %q", rec.Body.String())
	}
}

func TestShellIncludesSkipLinkAndActiveNavigationSemantics(t *testing.T) {
	component := layoutWithPage(pageContext{Title: "Tickets", ActiveRoute: "tickets", WorkspaceID: "workspace", ProjectID: "project"}, func(io.Writer) {})
	var output strings.Builder
	if err := component.Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	page := output.String()
	for _, fragment := range []string{`href="#main-content"`, `id="main-content"`, `aria-current="page"`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("expected %q in shell", fragment)
		}
	}
}
