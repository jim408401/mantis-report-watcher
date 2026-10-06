package mantis

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNoSOAPExtension(t *testing.T) {
	base := os.Getenv("MANTIS_NOEXT_URL")
	if base == "" {
		t.Skip("MANTIS_NOEXT_URL not set")
	}
	start := time.Now()
	c, u, err := Connect(context.Background(), Config{BaseURL: base + "/my_view_page.php", Username: "demo", Password: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("mode=%s user=%s in %v", c.Mode(), u.Name, time.Since(start))
	if c.Mode() != "rest-session" || time.Since(start) > 3*time.Second {
		t.Fatal("expected fast REST fallback")
	}
}
