package mantis

import (
	"context"
	"os"
	"testing"
)

// Runs against a server given by MANTIS_TEST_URL (e.g. the mock in tools/mockmantis).
func TestLive(t *testing.T) {
	base := os.Getenv("MANTIS_TEST_URL")
	if base == "" {
		t.Skip("MANTIS_TEST_URL not set")
	}
	ctx := context.Background()
	for _, cfg := range []Config{
		{BaseURL: base + "/search.php?project_id=0&handler_id=7", Username: "demo", Password: "demo"},
		{BaseURL: base, Username: "demo", Password: "demo", Mode: "rest"},
		{BaseURL: base, Token: "demotoken"},
	} {
		c, u, err := Connect(ctx, cfg)
		if err != nil {
			t.Fatalf("connect %+v: %v", cfg, err)
		}
		is, err := c.Issues(ctx, ScopeAssigned)
		if err != nil {
			t.Fatal(err)
		}
		fs, _ := c.Filters(ctx)
		f1001, err := c.Issues(ctx, "1001")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("mode=%s user=%+v issues=%d filters=%v f1001=%d first=%+v", c.Mode(), u, len(is), fs, len(f1001), is[0].Status)
		if len(is) == 0 || is[0].Summary == "" || is[0].Updated.IsZero() {
			t.Fatal("bad issues")
		}
	}
	_, _, err := Connect(ctx, Config{BaseURL: base, Username: "demo", Password: "x"})
	if KindOf(err) != KindAuth {
		t.Fatalf("want auth error, got %v", err)
	}
	t.Log("bad pw:", err)
	_, _, err = Connect(ctx, Config{BaseURL: "http://127.0.0.1:8099/nothing", Username: "demo", Password: "demo"})
	t.Log("not mantis:", KindOf(err), err)
	_, _, err = Connect(ctx, Config{BaseURL: "http://127.0.0.1:1/mantisbt", Username: "demo", Password: "demo"})
	t.Log("down:", KindOf(err), err)
}
