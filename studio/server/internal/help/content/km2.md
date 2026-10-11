---
title: KM2 knowledge
category: Configure
order: 37
permissions: connections.view, state.view, crews.chat
summary: How crews search, ask and save documents in KM2 (Knowledge Manager) through a profile-bound KM2 key, and how to set up that key so each functional area only reaches its own knowledge.
keywords: km2, knowledge manager, knowledge, knowledge base, search, ask, rag, document, save, external_ref, folder, default folder, profile, profile key, scoped key, api key, search:read, knowledge:write, knowledge:read, km_search, km_ask, km_save_document, Search Knowledge, Ask Knowledge, Save to Knowledge, km2:save, dry run
---
KM2 (Knowledge Manager) holds what should outlast a run: project charters, statements of work, decisions, meeting notes and weekly reports. Crews reach it through a **KM2 connection**. Crew Studio calls KM2 itself, so the KM2 key never reaches a run, and Crew Studio, not the agent, decides which key a run uses.

## Set up a KM2 profile key
<!-- permissions: connections.manage -->
A KM2 key acts as a KM2 **profile**: KM2 only lets it read and write what that profile may see, using KM2's own folder permissions. Make one key per functional area.

1. In KM2, sign in as an organization admin of the KM2 organization for Crew Studio.
2. Create an API key **bound to a non-admin profile** for the area, such as "Crew Studio – Finance": give it that area's region, department, role or group (KM2 calls this a scoped key; it reads exactly like a member holding those assignments). You can also limit it to the area's folders.
3. Grant the scopes **explicitly**: `search:read` (Search and Ask), `knowledge:write` (Save) and `knowledge:read` (Test and folder names). KM2 never grants `knowledge:write` through a wildcard.
4. Copy the key (it starts with `km2_`). KM2 shows it once.

Never use an organization-wide KM2 key. It skips KM2's folder permissions, so every crew using it could read everything in the organization.

## Connect KM2
<!-- permissions: connections.manage -->
On [Connections](/connections), click **New connection** and pick **KM2 (Knowledge Manager)**:

| Setting | What to enter |
|---|---|
| Base URL | KM2's API address, such as `https://km2.example.com` |
| Default folder for saves | The KM2 folder crews save to when they name none. **None (saving off)** turns saving off. |
| Other folders crews may save to | Optional further folders crews may name (at most 50). Crews may name only these and the default folder. |

When you create the connection, its key is not saved yet, so type the folder ids (or leave them empty): save the connection with its key, then click **Edit** and pick the folders from lists of their paths. A folder that is set but no longer visible to the key is kept and marked "(not visible to this key)". If the folders cannot be listed, the form says why (often a missing `knowledge:read`) and you can type the ids.
| Profile API key | The key from KM2 |

Pick the one **functional area** the key belongs to. Make **one KM2 connection per area**, and give each crew only one KM2 connection on its State tab: if a crew may use more than one, its KM2 tools refuse to run, because Crew Studio cannot tell which profile to act as.

Click **Test**. It lists the folders the key can see, marks the default folder and the other save folders, and warns when a save folder is not visible to the key.

![A tested KM2 connection listing the three folders its key can see, with the default folder for saves and a folder crews may save to marked.](connection-km2-test.png)

## Tools for crews
Give an agent these tools in the agent editor. They only work for crews with a KM2 connection.

| Tool | What it does |
|---|---|
| Search Knowledge (KM2) | Finds the passages most like a query, with their document titles |
| Ask Knowledge (KM2) | Asks KM2 a question and returns its answer with its sources |
| Save to Knowledge (KM2) | Saves a document (text, or a text file from the workspace) under an **external_ref** such as `atlas-weekly-2026-W41` |

Saving again with the same external_ref in the same folder replaces the earlier version instead of adding a copy, and saving identical content changes nothing. KM2 extracts facts from every saved document, which costs money on KM2's side, so save finished documents, such as a weekly report, rather than every draft.

Everything KM2 returns is handed to the agent marked as untrusted data, so text inside a document cannot give the crew instructions.

## What Crew Studio enforces
- **The key stays on the server.** Runs call Crew Studio with their run token, and Crew Studio calls KM2.
- **Folders.** A save goes to the default folder, or to a folder the connection lists. Any other folder is refused; KM2 then checks its own folder permissions as well.
- **Dry run.** In a dry run nothing is saved. The agent is told what would have been saved, and a dry-run entry is recorded.
- **The action log.** Every save is recorded as `km2:save` with its external_ref and KM2's outcome (created, updated, unchanged and so on). Agents cannot record `km2:` entries themselves.
- **Limits.** One run may save at most 10 documents, and one connection at most 200 a day. The same document saved twice in one run is sent once. Searches and questions are limited per run too, so a runaway agent cannot use up a key other crews share.
- **KM2's limits.** Documents are text of up to 1 MB. KM2 also caps how many documents a key may write a day; when that is used up, saves are refused until the next day.

## Where saves show
<!-- permissions: state.view -->
The crew's **State** tab lists its **Recent KM2 saves**: when, which document and external_ref, the connection and folder, the outcome (or "dry run, not sent", or "no answer from KM2" when KM2 failed after the document was sent), and the run that saved it. The full history is in the action log; filter it by `km2:save`.

![The Recent KM2 saves panel with a weekly report created in KM2, above the action log with its km2:save entry.](crew-state-action-log.png)

## In chat
<!-- permissions: crews.chat -->
When a crew may use a KM2 connection, its front desk can search KM2 and ask it questions for you, if you hold **View crew state**. It cannot save to KM2 from chat; start a work order for that.
