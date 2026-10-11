# CrewAI Agents — Quickstart

Set up a Python virtual environment for this project and install the CrewAI and LangChain libraries.

## Prerequisites

- Python 3.10–3.13 (CrewAI does not support other versions). This project uses Python 3.12.
- [uv](https://docs.astral.sh/uv/) (recommended). Install it with:

  ```bash
  curl -LsSf https://astral.sh/uv/install.sh | sh
  ```

- An OpenAI API key.

## 1. Create the virtual environment

From the project root:

```bash
uv venv --seed .venv
```

`--seed` installs `pip` into the environment, so `pip` also works after you activate it.

> **Without uv:** `python3 -m venv .venv`. On Debian/Ubuntu this fails with
> `ensurepip is not available` unless you first run `sudo apt install python3.12-venv`.

## 2. Activate it

```bash
source .venv/bin/activate
```

Your prompt now starts with `(.venv)`. Activation only lasts for the current terminal, so run this command in each new terminal. To leave the environment, run `deactivate`.

**VS Code:** press `Ctrl+Shift+P`, choose **Python: Select Interpreter**, and pick `./.venv/bin/python`. New terminals in VS Code then activate the environment automatically.

## 3. Install the libraries

```bash
uv pip install crewai python-dotenv langchain_openai langchain_community
uv pip install 'crewai[tools]'
```

Plain `pip install ...` works too; `uv pip` is just faster. Keep the quotes around `'crewai[tools]'`: without them, zsh treats the square brackets as a filename pattern and the command fails.

**Running the Crew Studio stack ([section 6](#6-run-the-crew-studio-stack))?** Install the pinned environment instead. It has everything above plus `crewai-files` and `anthropic`, which the stack's runner needs:

```bash
uv pip install -r studio/runner/requirements.txt
```

## Installed libraries

| Package | Version | Purpose |
|---|---|---|
| `crewai` | 1.15.23 | Framework for building and running teams of AI agents |
| `crewai-tools` (via `crewai[tools]`) | 1.15.23 | Ready-made tools for agents: web scraping, file, PDF, DOCX and YouTube readers, search |
| `python-dotenv` | 1.2.4 | Loads environment variables (API keys) from a `.env` file |
| `langchain-openai` | 1.6.7 | OpenAI chat and embedding models for LangChain |
| `langchain-community` | 0.4.2 | Community integrations for LangChain |
| `langchain-core` | 1.6.6 | Installed as a dependency of the two LangChain packages above |

Notes:

- `langchain-community` prints a deprecation warning when you import it. It still works, but it is no longer maintained, so prefer the standalone integration packages (such as `langchain-openai`) where you can.
- CrewAI can call OpenAI through its own `LLM` class, so `langchain-openai` is optional for CrewAI-only code.

To check that everything is installed and compatible:

```bash
uv pip check
python -c "import crewai, crewai_tools, dotenv, langchain_openai, langchain_community; print('OK')"
```

## 4. Add your API keys

Create a `.env` file in the project root:

```env
OPENAI_API_KEY=sk-...
GEMINI_API_KEY=...
ANTHROPIC_API_KEY=sk-ant-...
```

- `OPENAI_API_KEY` runs the agents.
- `ANTHROPIC_API_KEY` is only needed for agents that use Claude models in Crew Studio.
- `GEMINI_API_KEY` powers web search ([google_search_tool.py](google_search_tool.py)), which uses Gemini's Google Search grounding to return an answer plus source links. Either key type works, and the tool picks the endpoint from the key:
  - **Vertex AI key** (starts with `AQ.`): in the Google Cloud console, enable the Vertex AI API, then create a key there.
  - **Gemini API key** (starts with `AIza`): create one in AI Studio, or in the Cloud console by enabling the **Gemini API** and creating an API key restricted to it.
- Gemini 2.5 Flash includes 1,500 free grounded searches per day, then $35 per 1,000. To use a different model, set `GEMINI_SEARCH_MODEL` in `.env`.

### Spend cap

[budget_guard.py](budget_guard.py) caps the estimated spend of each run. It is $2.00 by default; to change it, add this to `.env`:

```env
RUN_BUDGET_USD=1.50
```

When a run reaches the budget, searches stop and agents write their answers from what they already have. At 125% of the budget, every further model call is blocked and the run ends. A long research script can still save the sections that finished. The guard only estimates spend, so also set spend limits in the OpenAI and Google Cloud consoles.

Never commit `.env`. Add it to `.gitignore`:

```bash
echo ".env" >> .gitignore
echo ".venv/" >> .gitignore
```

Load the key in code:

```python
from dotenv import load_dotenv

load_dotenv()  # reads .env into os.environ
```

## 5. Minimal example

```python
from dotenv import load_dotenv
from crewai import Agent, Task, Crew

load_dotenv()

researcher = Agent(
    role="Researcher",
    goal="Summarize a topic clearly",
    backstory="An analyst who writes concise briefs.",
)

task = Task(
    description="Write a three-bullet summary of what CrewAI is.",
    expected_output="Three bullet points.",
    agent=researcher,
)

crew = Crew(agents=[researcher], tasks=[task])
print(crew.kickoff())
```

Run it with the environment active:

```bash
python main.py
```

## 6. Run the Crew Studio stack

Crew Studio ([studio/](studio/)) is a browser UI for designing, running and inspecting crews. The stack is
Postgres and Azurite (local blob storage) in Docker, a Go API that also starts the Python runner, and a React UI.

### Prerequisites

- Everything above: the `.venv` with the pinned environment (`studio/runner/requirements.txt`) and a root `.env`
  holding your provider keys. Crew Studio passes those keys to the runner and never stores or shows them.
- Docker with Compose v2 (`docker compose`), running.
- Go 1.24+ and Node 20+ (with npm).
- `openssl`, `curl` and `setsid`. They are already installed on most Linux systems.
- Free ports: 8088 (API and UI), 5174 (dev UI), 55432 (Postgres) and 10000 (Azurite). All of them listen on
  loopback only.

### Start it

From the project root:

```bash
./run-stack.sh          # build everything and serve UI + API at http://localhost:8088
./run-stack.sh --dev    # API on :8088 plus the Vite dev server with hot reload at http://localhost:5174
./run-stack.sh --stop   # stop the Postgres and Azurite containers (data is kept)
```

On the first run, the script:

1. Creates `studio/.env` from `studio/.env.example` with a random database password.
2. Starts the Postgres and Azurite containers.
3. Builds the Go service, installs the UI dependencies and builds the UI.
4. Starts the service, which applies the database migrations.

**On the very first start, the service prints a one-time link** (`/setup?token=...`). Open it to create the first
administrator, then sign in with that account. Later starts skip the setup link and the dependency install, so they
are faster.

Ctrl+C stops the API (and the dev server). The containers keep running so the next start is quick; stop them with
`./run-stack.sh --stop`.

### Optional settings

Edit `studio/.env` to change these, then restart the stack:

- **Single sign-on** (Microsoft Entra ID or Clerk): fill in the `STUDIO_MICROSOFT_*` or `STUDIO_CLERK_*` values.
  Without them, administrators sign in with a password.
- **Connections to Jira, Linear, KM2 and other outside systems** need `STUDIO_SECRETS_KEY`, which encrypts their
  secrets. Generate one with `openssl rand -base64 32` and back it up with the database: once connections exist,
  the service will not start without it.
- **Help search** uses OpenAI embeddings by default. Set `STUDIO_HELP_SEARCH=keyword` to avoid external calls.

### Run the steps yourself

The `make` targets in `studio/` do the same steps one at a time (`make help` lists them all):

```bash
cd studio
make setup               # studio/.env, Postgres, test database, UI dependencies, build, migrations
make start               # build and serve UI + API at http://127.0.0.1:8088
make dev-api             # or: API only, in one terminal...
make dev-web             # ...and the Vite dev UI at http://127.0.0.1:5174 in another
make test                # Go, Python runner and web test suites
```

### Troubleshooting

| Message | Fix |
|---|---|
| `Docker is not running` | Start Docker (`sudo systemctl start docker`) and run the script again. |
| `Python venv not found` / `crewai is not installed` / `crewai-files is not installed` | Do steps 1–3, installing from `studio/runner/requirements.txt`. |
| `port 8088 is already in use` | Crew Studio is already running; stop it with Ctrl+C in that terminal. |
| `The containers did not start` | Run `docker compose -f studio/docker-compose.yml --env-file studio/.env up` to see the error. |
| A crew will not run | Its page lists what is missing, such as a model price or a provider key in `.env`. |

For how the pieces fit together and how a run works, see
[studio/docs/architecture.md](studio/docs/architecture.md). For roles and areas, work orders, the integration API
and security notes, see [studio/README.md](studio/README.md).
