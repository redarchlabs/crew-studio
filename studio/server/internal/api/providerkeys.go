package api

import (
	"fmt"

	"crewstudio/internal/model"
	"crewstudio/internal/runspec"
)

// missingProviderKeys maps each model whose provider key is not set to that key's variable name. It only warns:
// a key may also reach runs some other way, and an unreadable env file (or a catalog that cannot tell) gives no
// warnings.
func (s *Server) missingProviderKeys(models []string) map[string]string {
	checker, ok := s.tools.(runspec.KeyChecker)
	if !ok {
		return map[string]string{}
	}
	missing, err := runspec.MissingProviderKeys(checker, models)
	if err != nil {
		s.log.Warn("could not check provider keys", "err", err)
		return map[string]string{}
	}
	return missing
}

func providerKeyWarning(env string) string {
	return fmt.Sprintf("%s is not set in the provider env file, so runs on this model will fail to authenticate; whoever runs Crew Studio can add it.", env)
}

// crewKeyWarnings warns about each LLM the crew uses whose provider key is missing.
func (s *Server) crewKeyWarnings(crew model.Crew, lib runspec.Library) []string {
	ids := []string{}
	if crew.ManagerLLMID != nil {
		ids = append(ids, *crew.ManagerLLMID)
	}
	for _, agentID := range crew.AgentIDs {
		if a, ok := lib.Agents[agentID]; ok && a.LLMID != nil {
			ids = append(ids, *a.LLMID)
		}
	}
	var llms []model.LLM
	seen := map[string]bool{}
	for _, id := range ids {
		if l, ok := lib.LLMs[id]; ok && !seen[id] {
			seen[id] = true
			llms = append(llms, l)
		}
	}
	models := make([]string, len(llms))
	for i, l := range llms {
		models[i] = l.Model
	}
	missing := s.missingProviderKeys(models)
	out := []string{}
	for _, l := range llms {
		if env := missing[l.Model]; env != "" {
			out = append(out, fmt.Sprintf("LLM %s (%s): %s", l.Name, l.Model, providerKeyWarning(env)))
		}
	}
	return out
}
