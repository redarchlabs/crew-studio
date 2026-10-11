package runner

import (
	"testing"
	"time"
)

// A specialist's question to the lead (slice 3c) is a runner event the journal shows, not a log line.
func TestTranslateStdoutKeepsConsultations(t *testing.T) {
	got, ok := translateStdout(`{"type":"consultation","payload":{"from_agent":"Analyst","to_agent":"Lead","question":"Prices?","outcome":"asked"}}`, time.Now())
	if !ok || got.Event.Type != "consultation" || got.Event.Payload["from_agent"] != "Analyst" {
		t.Errorf("got %+v", got.Event)
	}
}
