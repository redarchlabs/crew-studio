package model

import "strings"

// Entries Crew Studio records itself, which agents may not record with Record Action (log_record): otherwise an
// agent (or text injected into it) could record a tracker post or a report delivery that never happened, mark a
// tracker follow-up as sent so the real reminder is never posted, or take the idempotency key a real post or
// delivery will use so it is skipped as "already done".
var studioActionPrefixes = []string{"tracker:", "report:"}

// studioKeyPrefixes start the idempotency keys of tracker posts (run:<run id>:...) and report deliveries.
var studioKeyPrefixes = []string{"run:", "report:"}

// checkStudioLogRecord adds the problems of an agent-recorded entry that only Studio may record.
func checkStudioLogRecord(c *checker, in LogRecord) {
	action := strings.ToLower(in.Action)
	for _, p := range studioActionPrefixes {
		if strings.HasPrefix(action, p) {
			c.addf("action", "%s actions are recorded by Crew Studio's tools only (the tracker tools and Deliver Report)", p)
		}
	}
	if strings.HasPrefix(action, "followup:") && isTrackerSubject(in.Subject) {
		c.addf("action", "follow-ups of tracker issues are recorded by Comment on Tracker Issue with followup_step, when the comment is posted")
	}
	key := strings.ToLower(in.IdempotencyKey)
	for _, p := range studioKeyPrefixes {
		if strings.HasPrefix(key, p) {
			c.addf("idempotency_key", "must not start with %s", p)
		}
	}
}

// isTrackerSubject reports whether a subject names a tracker issue, such as jira:PRJ-15.
func isTrackerSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	return strings.HasPrefix(s, TrackerJira+":") || strings.HasPrefix(s, TrackerLinear+":") || strings.HasPrefix(s, TrackerKM2+":")
}
