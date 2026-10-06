package mantis

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"http://srv/mantisbt/search.php?project_id=0&handler_id=5": "http://srv/mantisbt",
		"srv/mantisbt/":                        "http://srv/mantisbt",
		"https://srv/login_page.php":           "https://srv",
		"http://srv:8080/bugs/api/rest/issues": "http://srv:8080/bugs",
		"http://srv/mantisbt/view.php?id=12":   "http://srv/mantisbt",
	}
	for in, want := range cases {
		got, err := NormalizeBaseURL(in)
		if err != nil || got != want {
			t.Errorf("%s => %s (%v), want %s", in, got, err, want)
		}
	}
}
