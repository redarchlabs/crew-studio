package trackertest

import (
	"encoding/base64"
	"sort"
)

func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func sortStrings(s []string) { sort.Strings(s) }
