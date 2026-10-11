// Crew state (notes and the action log) and connections to outside systems. See docs/crew-state-and-pmo-design.md.

/** mcp is an outside MCP server whose reading tools agents may use in chat (not Studio's own MCP endpoint) */
export type ConnectionKindKey = 'jira' | 'linear' | 'km2_tracker' | 'km2' | 'http' | 'mcp' | 'slack' | 'teams' | 'email'

/**
 * toggle: an on/off setting stored as "true" or left out; cloud_id: an Atlassian cloud id (a UUID); uuid: a KM2 folder
 * id; uuid_list: KM2 folder ids separated by commas; select: one of the field's options
 */
export type ConnectionFieldInput = 'text' | 'url' | 'email' | 'port' | 'header' | 'https_url' | 'toggle' | 'cloud_id' | 'uuid' | 'uuid_list' | 'select'

export type ConnectionField = {
  key: string
  label: string
  input: ConnectionFieldInput
  secret: boolean
  required: boolean
  hint?: string
  /** The choices of a select field */
  options?: string[]
  /** When set, the value must start with it (KM2 keys start with km2_) */
  prefix?: string
}

export type ConnectionKind = {
  kind: ConnectionKindKey
  label: string
  fields: ConnectionField[]
  /** At least one of these secrets must be set (Slack: a webhook or a bot token) */
  any_secret?: string[]
  /** "Test connection" can reach this kind today */
  testable: boolean
}

export type ConnectionKinds = {
  kinds: ConnectionKind[]
  /** False when the server has no STUDIO_SECRETS_KEY, so connections cannot be added */
  secrets_ready: boolean
}

/** A connection as the API returns it: settings and which secrets are set, never the secret values. */
export type Connection = {
  id: string
  name: string
  kind: ConnectionKindKey
  config: Record<string, string>
  secret_keys: string[]
  missing_secrets: string[]
  secret_updated_at: string | null
  area_ids: string[]
  enabled: boolean
  crew_count: number
  created_by: string | null
  created_at: string
  updated_at: string
}

export type ConnectionInput = {
  name: string
  kind: ConnectionKindKey
  config: Record<string, string>
  area_ids: string[]
  enabled: boolean
  /** Only when creating; afterwards secrets are replaced through their own call */
  secrets?: Record<string, string>
}

/** What a tool of an MCP connection does: only enabled tools that read are given in agent chat */
export type ToolEffectChoice = 'reads' | 'changes'

/**
 * A tool of an MCP connection, as Discover tools last found it. name, description and input_schema are the MCP server's
 * own words: untrusted, shown as plain text only.
 */
export type ConnectionTool = {
  connection_id: string
  name: string
  description: string
  input_schema: Record<string, unknown>
  effect: ToolEffectChoice
  enabled: boolean
  discovered_at: string
  updated_by_name: string
  updated_at: string
}

export type ConnectionToolChange = { name: string; effect: ToolEffectChoice; enabled: boolean }

/**
 * Discover tools: the tools kept, how many the server listed, why the others were left out, and the tools that are new
 * or changed since the last discovery (they start as Changes until someone marks them Reads)
 */
export type DiscoverToolsResult = { tools: ConnectionTool[]; found: number; skipped: string[]; review: string[] }

export type ConnectionTestResult = {
  supported: boolean
  ok: boolean
  message: string
  status_code?: number
  duration_ms: number
  /** KM2 only: the folders the key can see (the first 200), how many there are, and problems with the save folders */
  folders?: KM2Folder[]
  folder_count?: number
  warnings?: string[]
  /** Email only: where the test message went */
  sent_to?: string
}

/** The folders a KM2 connection's key can see, for pickers, with the folders the connection allows. */
export type KM2FolderList = {
  ok: boolean
  /** Why the list could not be read when ok is false (such as the key lacking knowledge:read) */
  message: string
  status_code?: number
  folders: KM2Folder[]
  folder_count: number
  allowed: { default_folder_id: string; write_folder_ids: string[] }
  warnings: string[]
}

/** A KM2 folder as a KM2 connection test lists it. */
export type KM2Folder = { id: string; name: string; path: string; parent_id: string | null }

/** The latest version of a note, without its text. */
export type NoteInfo = {
  schedule_id: string | null
  schedule_name: string
  subject: string
  name: string
  version: number
  bytes: number
  written_by: string
  run_id: string | null
  updated_at: string
}

export type NoteVersion = {
  id: string
  crew_id: string
  schedule_id: string | null
  subject: string
  name: string
  version: number
  content: string
  run_id: string | null
  written_by: string
  created_at: string
}

/** Identifies one note of a crew: its schedule (null for crew-wide), subject and name. */
export type NoteKey = { schedule_id: string | null; subject: string; name: string }

export type ActionEntry = {
  id: string
  crew_id: string
  schedule_id: string | null
  schedule_name: string
  subject: string
  action: string
  run_id: string | null
  dry_run: boolean
  details: Record<string, unknown>
  idempotency_key: string | null
  at: string
}
