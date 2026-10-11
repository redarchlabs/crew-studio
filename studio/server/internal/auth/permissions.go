// Package auth holds the permission catalog, the signed-in principal, password hashing, tokens and SSO.
package auth

// Permission keys. The same catalog is seeded in migrations 0004, 0008, 0012 and 0014; TestCatalogMatchesDatabase keeps them in step.
const (
	OverviewView = "overview.view"

	CrewsView   = "crews.view"
	CrewsCreate = "crews.create"
	CrewsEdit   = "crews.edit"
	CrewsDelete = "crews.delete"
	CrewsRun    = "crews.run"
	// CrewsChat lets a person chat with crews that have chat on and confirm what their front desk proposes
	CrewsChat = "crews.chat"

	// StateView and StateManage cover crews' action logs and notes
	StateView   = "state.view"
	StateManage = "state.manage"

	AgentsView   = "agents.view"
	AgentsCreate = "agents.create"
	AgentsEdit   = "agents.edit"
	AgentsDelete = "agents.delete"

	TasksView   = "tasks.view"
	TasksCreate = "tasks.create"
	TasksEdit   = "tasks.edit"
	TasksDelete = "tasks.delete"

	WorkOrdersView   = "work_orders.view"
	WorkOrdersCreate = "work_orders.create"
	WorkOrdersCancel = "work_orders.cancel"
	WorkOrdersDelete = "work_orders.delete"

	RunsView   = "runs.view"
	RunsCancel = "runs.cancel"
	RunsDelete = "runs.delete"

	SchedulesView   = "schedules.view"
	SchedulesManage = "schedules.manage"
	TriggersView    = "triggers.view"
	TriggersManage  = "triggers.manage"

	LLMsView       = "llms.view"
	LLMsManage     = "llms.manage"
	PricingView    = "pricing.view"
	PricingManage  = "pricing.manage"
	SettingsView   = "settings.view"
	SettingsManage = "settings.manage"
	HistoryView    = "history.view"

	ConnectionsView   = "connections.view"
	ConnectionsManage = "connections.manage"

	// AccountAPIKeys lets a person create API keys that act as themselves
	AccountAPIKeys = "account.api_keys"

	AdminUsers           = "admin.users"
	AdminServiceAccounts = "admin.service_accounts"
	AdminRoles           = "admin.roles"
	AdminAreas           = "admin.areas"
	AdminAccess          = "admin.access"
	AdminAudit           = "admin.audit"
)

// Catalog lists every permission in display order.
var Catalog = []string{
	OverviewView,
	CrewsView, CrewsCreate, CrewsEdit, CrewsDelete, CrewsRun, CrewsChat,
	StateView, StateManage,
	AgentsView, AgentsCreate, AgentsEdit, AgentsDelete,
	TasksView, TasksCreate, TasksEdit, TasksDelete,
	WorkOrdersView, WorkOrdersCreate, WorkOrdersCancel, WorkOrdersDelete,
	RunsView, RunsCancel, RunsDelete,
	SchedulesView, SchedulesManage, TriggersView, TriggersManage,
	LLMsView, LLMsManage, PricingView, PricingManage, SettingsView, SettingsManage,
	ConnectionsView, ConnectionsManage,
	HistoryView,
	AccountAPIKeys,
	AdminUsers, AdminServiceAccounts, AdminRoles, AdminAreas, AdminAccess, AdminAudit,
}

// Known reports whether key is in the catalog.
func Known(key string) bool {
	for _, k := range Catalog {
		if k == key {
			return true
		}
	}
	return false
}

// Fixed ids of rows seeded by migration 0004.
const (
	AdministratorRoleID = "66666666-0000-4000-8000-000000000001"
)
