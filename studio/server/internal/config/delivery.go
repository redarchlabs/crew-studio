package config

import (
	"fmt"
	"net/url"
	"strings"
)

// deliverySettings reads STUDIO_DELIVERY_ORIGINS (origins such as https://hooks.example.com, or http on loopback for
// tests) and checks STUDIO_SLACK_API_URL.
func deliverySettings(list, slackURL string) ([]string, []string) {
	var origins, problems []string
	for _, origin := range strings.Split(list, ",") {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || !secureOrLoopback(u) || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" {
			problems = append(problems, fmt.Sprintf("STUDIO_DELIVERY_ORIGINS: %q must be an https origin such as https://hooks.example.com (http only for loopback)", origin))
			continue
		}
		origins = append(origins, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	if slackURL != "" {
		if u, err := url.Parse(slackURL); err != nil || !secureOrLoopback(u) || u.Host == "" || u.User != nil {
			problems = append(problems, "STUDIO_SLACK_API_URL must be an https URL (http only for loopback)")
		}
	}
	return origins, problems
}
