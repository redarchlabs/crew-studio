package config

import (
	"slices"
	"testing"
)

func TestDeliverySettings(t *testing.T) {
	origins, problems := deliverySettings(" http://127.0.0.1:8795/ , https://Hooks.Example.com,,", "http://localhost:8795/slack/api")
	if len(problems) != 0 || !slices.Equal(origins, []string{"http://127.0.0.1:8795", "https://hooks.example.com"}) {
		t.Errorf("origins = %v, problems = %v", origins, problems)
	}
	_, problems = deliverySettings("http://hooks.example.com,https://x.example.com/path,https://u:p@x.example.com", "http://slack.example.com")
	if len(problems) != 4 {
		t.Errorf("problems = %v", problems)
	}
}
