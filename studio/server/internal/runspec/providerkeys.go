package runspec

import "crewstudio/internal/model"

// KeyChecker tells which variables are set in the provider env file or the server's environment (runner.Catalog
// reads the file for that). Values are never read out.
type KeyChecker interface {
	EnvSet(keys ...string) (map[string]bool, error)
}

// MissingProviderKeys maps each model whose provider key is not set to that key's variable name. Models whose
// provider Crew Studio does not know are left out, as is everything when none has a known provider.
func MissingProviderKeys(checker KeyChecker, models []string) (map[string]string, error) {
	out := map[string]string{}
	need := map[string]string{}
	var keys []string
	for _, m := range models {
		if env := model.ProviderKeyEnv(m); env != "" {
			need[m] = env
			keys = append(keys, env)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	set, err := checker.EnvSet(keys...)
	if err != nil {
		return out, err
	}
	for m, env := range need {
		if !set[env] {
			out[m] = env
		}
	}
	return out, nil
}
