package model

// InputRunTasks is the work order input that picks which of the crew's tasks run (task ids or names, separated by
// commas). Crews need not declare it; without it every task runs. Schedules use it to run one job of a crew with
// several, such as the PMO's daily sweep or weekly report.
const InputRunTasks = "run_tasks"

// IsUUID reports whether s is a UUID in its usual text form.
func IsUUID(s string) bool { return uuidPattern.MatchString(s) }
