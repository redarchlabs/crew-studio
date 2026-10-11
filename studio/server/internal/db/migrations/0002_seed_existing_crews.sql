-- Imports the crews that were first hand-written in Python (a private-company due diligence script and
-- research_file.py) so they can be edited and run from Crew Studio.

-- +goose Up
-- +goose StatementBegin
INSERT INTO model_prices (model, input_per_mtok, cached_input_per_mtok, output_per_mtok, notes) VALUES
    ('gpt-6-luna', 0.10, 0.01, 0.50, 'https://openai.com/api/pricing'),
    ('gemini-2.5-flash', 0.30, 0.03, 2.50, 'https://cloud.google.com/vertex-ai/generative-ai/pricing');

INSERT INTO llms (id, name, model, temperature, max_completion_tokens) VALUES
    ('11111111-0000-4000-8000-000000000001', 'GPT-6 Luna (research)', 'openai/gpt-6-luna', NULL, 16384),
    ('11111111-0000-4000-8000-000000000002', 'GPT-6 Luna (memo)', 'openai/gpt-6-luna', NULL, 32768),
    ('11111111-0000-4000-8000-000000000003', 'GPT-6 Luna (short)', 'openai/gpt-6-luna', NULL, 4096);

INSERT INTO agents (id, name, role, goal, backstory, llm_id, tools, max_iter, allow_delegation, verbose_output) VALUES
    ('22222222-0000-4000-8000-000000000001', $s$Corporate Investigator$s$, $s$Corporate Investigator$s$, $s$Establish exactly who the target company is, who owns it, and how it is structured$s$, $s$A former due diligence investigator at a corporate intelligence firm. You start with state business registries and work outward, and you never confuse two companies that share a name.$s$, '11111111-0000-4000-8000-000000000001', '{google_search}', 12, false, true),
    ('22222222-0000-4000-8000-000000000002', $s$Financial Analyst$s$, $s$Private Company Financial Analyst$s$, $s$Build the best available picture of the target's revenue, costs, contracts, and debt$s$, $s$A private equity associate who sizes private companies without audited financials. You piece revenue together from public contract awards, procurement records, government loan data, and lien filings, and you show your math.$s$, '11111111-0000-4000-8000-000000000001', '{google_search}', 12, false, true),
    ('22222222-0000-4000-8000-000000000003', $s$Risk Investigator$s$, $s$Safety, Legal and Compliance Investigator$s$, $s$Find every accident, safety violation, lawsuit, and regulatory problem tied to the target$s$, $s$A risk investigator who has vetted service companies for insurers and buyers. You search regulatory records, court dockets, labor regulators, and local news, because the worst liabilities rarely appear in a seller's data room.$s$, '11111111-0000-4000-8000-000000000001', '{google_search}', 12, false, true),
    ('22222222-0000-4000-8000-000000000004', $s$Market Analyst$s$, $s$Industry and Market Analyst$s$, $s$Place the target in its market: competitors, customer sentiment, trends, and deal comparables$s$, $s$A sector analyst covering outsourced business services. You know the large consolidators, track recent acquisitions and their multiples, and read employee and customer reviews for early warning signs.$s$, '11111111-0000-4000-8000-000000000001', '{google_search}', 12, false, true),
    ('22222222-0000-4000-8000-000000000005', $s$Deal Partner$s$, $s$Private Equity Deal Partner$s$, $s$Decide whether the target is worth pursuing and what must be verified before signing$s$, $s$A partner at a lower middle market private equity firm who has closed many service-business acquisitions. You are skeptical, separate facts from estimates, and lead with the risks that could kill a deal.$s$, '11111111-0000-4000-8000-000000000002', '{}', 25, false, true),
    ('22222222-0000-4000-8000-000000000006', 'Research Analyst', 'Senior Research Analyst', 'Uncover cutting-edge developments in AI', $s$You are an experienced research analyst with a keen eye for emerging trends in technology.$s$, '11111111-0000-4000-8000-000000000003', '{google_search}', 25, false, true);

INSERT INTO tasks (id, name, description, expected_output, agent_id, markdown) VALUES
    ('33333333-0000-4000-8000-000000000001', $s$Corporate Profile$s$, $s$Research {company} (location hint, may be blank: {location}) for a private equity firm considering buying it.

Cover:
1. Identity: what the company does (confirm the line of business), website, headquarters, and every state, region, or city it operates in. List any other entities with the same or similar name and how you told them apart.
2. Registration: state(s) of formation, formation date, entity status (active, delinquent, dissolved), registered agent, filing numbers, and any DBAs, subsidiaries, or affiliated entities.
3. Ownership and leadership: owners, members, managers, officers, and key executives, with their backgrounds. Is it founder-owned, family-owned, or already backed by private equity or a strategic parent?
4. History: founding story, growth, acquisitions it made, divestitures, rebrands, and prior sale attempts.
5. Size: employee count, key equipment or fleet, and facilities (owned or leased).

Evidence rules:
- Cite a source URL for every fact. If you cannot find a source, say so.
- Label every item CONFIRMED (primary source such as a government record, court filing, or the company itself), REPORTED (news or a third-party database), or ESTIMATED (your own calculation; show the math).
- Several companies may share this name. Before reporting a fact, check it belongs to the same entity (same state, address, or principals). Keep facts about other entities apart and flag them.
- Write 'Not found' rather than guessing. A gap is useful diligence output.
- Be efficient: run targeted searches, stop once a topic is covered, and leave enough effort to write the full answer.$s$, $s$A markdown corporate profile with a section for each of the five areas, every item labeled CONFIRMED, REPORTED, or ESTIMATED with its source URL, and a final 'Name collisions and unresolved identity questions' section.$s$, '22222222-0000-4000-8000-000000000001', true),
    ('33333333-0000-4000-8000-000000000002', $s$Financial Picture$s$, $s$Build the financial picture of {company} (location hint, may be blank: {location}). It is likely private, so assemble the numbers from public records.

Cover:
1. Revenue: any reported figures, third-party estimates (Dun & Bradstreet, ZoomInfo, and similar), and your own estimate built from contracts and headcount. Give a low, base, and high case and show the math.
2. Customer contracts: each major customer, contract value per year, term, start and renewal dates, how it was awarded (bid or sole source), and any rate increases. Search public meeting minutes, budgets, RFP awards, and procurement portals.
3. Customer concentration: share of revenue from the largest customers and any contracts up for rebid soon.
4. Costs and spending: staff wage rates, equipment purchases, insurance, and any capital spending visible in public records.
5. Debt and government money: PPP or SBA loans (amount, lender, jobs reported, forgiveness), government grants, UCC filings, tax liens, judgments, and bankruptcies.

Evidence rules:
- Cite a source URL for every fact. If you cannot find a source, say so.
- Label every item CONFIRMED (primary source such as a government record, court filing, or the company itself), REPORTED (news or a third-party database), or ESTIMATED (your own calculation; show the math).
- Several companies may share this name. Before reporting a fact, check it belongs to the same entity (same state, address, or principals). Keep facts about other entities apart and flag them.
- Write 'Not found' rather than guessing. A gap is useful diligence output.
- Be efficient: run targeted searches, stop once a topic is covered, and leave enough effort to write the full answer.$s$, $s$A markdown financial section with a revenue table (low, base, high with assumptions), a contract table (customer, annual value, term, renewal date, source), and subsections for costs, debt, liens, and government funds. Every number labeled CONFIRMED, REPORTED, or ESTIMATED with its source.$s$, '22222222-0000-4000-8000-000000000002', true),
    ('33333333-0000-4000-8000-000000000003', $s$Safety, Legal and Compliance$s$, $s$Investigate every safety, legal, and regulatory risk tied to {company} (location hint, may be blank: {location}).

Cover:
1. Accidents and incidents: every accident, injury, service failure, and similar incident in the news or public records, with dates, locations, and outcomes.
2. Regulatory record: licenses and registrations the business needs, inspection results, safety ratings, and any enforcement actions or alerts from the regulators that oversee its industry.
3. People issues: arrests, misconduct allegations, background check failures, and licensing problems involving employees or management.
4. Litigation: lawsuits where the company is a defendant or plaintiff, settlements, and verdicts. Search state and federal court records and news.
5. Labor and regulators: OSHA citations, Department of Labor wage cases, NLRB filings, union organizing or strikes, EEOC complaints, and violations found by state regulators.
6. Contract disputes: customers that fined, warned, or dropped the company, and complaints raised in public meetings or filings.

Evidence rules:
- Cite a source URL for every fact. If you cannot find a source, say so.
- Label every item CONFIRMED (primary source such as a government record, court filing, or the company itself), REPORTED (news or a third-party database), or ESTIMATED (your own calculation; show the math).
- Several companies may share this name. Before reporting a fact, check it belongs to the same entity (same state, address, or principals). Keep facts about other entities apart and flag them.
- Write 'Not found' rather than guessing. A gap is useful diligence output.
- Be efficient: run targeted searches, stop once a topic is covered, and leave enough effort to write the full answer.$s$, $s$A markdown risk register: a table of each incident or issue (date, category, description, severity HIGH/MEDIUM/LOW, status, source), followed by the regulatory summary and a short read on overall risk.$s$, '22222222-0000-4000-8000-000000000003', true),
    ('33333333-0000-4000-8000-000000000004', $s$Market and Competition$s$, $s$Place {company} (location hint, may be blank: {location}) in its market.

Cover:
1. Market: size and growth of the company's industry in its markets, and how much customers outsource this work versus doing it in-house.
2. Competitors: national players and local rivals in each market, plus contracts the company won from or lost to them.
3. Reputation: employee reviews (Glassdoor, Indeed), customer complaints, BBB rating, and social media sentiment.
4. Trends that matter for a buyer: labor supply and wages, regulation and funding changes, demand shifts, and contract pricing trends.
5. Deal comparables: recent acquisitions of comparable companies, buyers, and any disclosed prices or EBITDA multiples.

Evidence rules:
- Cite a source URL for every fact. If you cannot find a source, say so.
- Label every item CONFIRMED (primary source such as a government record, court filing, or the company itself), REPORTED (news or a third-party database), or ESTIMATED (your own calculation; show the math).
- Several companies may share this name. Before reporting a fact, check it belongs to the same entity (same state, address, or principals). Keep facts about other entities apart and flag them.
- Write 'Not found' rather than guessing. A gap is useful diligence output.
- Be efficient: run targeted searches, stop once a topic is covered, and leave enough effort to write the full answer.$s$, $s$A markdown market section with a competitor table, a reputation summary, the key trends with their effect on the target, and a table of comparable transactions with sources.$s$, '22222222-0000-4000-8000-000000000004', true),
    ('33333333-0000-4000-8000-000000000005', $s$Investment Memo$s$, $s$Write the due diligence memo on {company} for the investment committee, using only the four research sections you were given. Do not add facts they do not contain.

Sections, in this order:
1. Executive summary: what the company is, the headline numbers, the top three risks, and your recommendation (pursue, pursue with conditions, or pass) with the reasons.
2. Company profile and ownership.
3. Financials: revenue range, contracts, concentration, costs, debt.
4. Safety, accidents and legal: the full risk register.
5. Market position and competition.
6. Valuation considerations: what multiples comparables suggest and what would move the price up or down.
7. Red flags and deal breakers.
8. Information gaps: what public research could not answer.
9. Data request list for management: specific documents to ask for in the data room (financial statements, contracts, insurance loss runs, employee files, and so on).
10. Sources: every URL cited, grouped by section.

Keep the CONFIRMED, REPORTED, and ESTIMATED labels. If the researchers found that several companies share the name, say which one the memo covers.$s$, $s$A complete markdown investment memo with the ten sections above, tables where they help, and every claim traceable to a source.$s$, '22222222-0000-4000-8000-000000000005', true),
    ('33333333-0000-4000-8000-000000000006', 'AI Research Task', $s$Conduct a comprehensive investigation into the latest advancements in artificial intelligence, focusing on breakthroughs, applications, and emerging trends.$s$, $s$A bullet-point list of the 5-10 most important recent AI developments, each with a one-line summary and its source.$s$, '22222222-0000-4000-8000-000000000006', false),
    ('33333333-0000-4000-8000-000000000007', 'AI Research Report', $s$Compile the findings from the research task into a detailed report, highlighting key insights, trends, and potential implications for the field of artificial intelligence.$s$, $s$A well-structured markdown report with an introduction, key insights, trends, implications, and a sources section.$s$, '22222222-0000-4000-8000-000000000006', true);

INSERT INTO task_context (task_id, context_task_id, position) VALUES
    ('33333333-0000-4000-8000-000000000002', '33333333-0000-4000-8000-000000000001', 0),
    ('33333333-0000-4000-8000-000000000003', '33333333-0000-4000-8000-000000000001', 0),
    ('33333333-0000-4000-8000-000000000004', '33333333-0000-4000-8000-000000000001', 0),
    ('33333333-0000-4000-8000-000000000005', '33333333-0000-4000-8000-000000000001', 0),
    ('33333333-0000-4000-8000-000000000005', '33333333-0000-4000-8000-000000000002', 1),
    ('33333333-0000-4000-8000-000000000005', '33333333-0000-4000-8000-000000000003', 2),
    ('33333333-0000-4000-8000-000000000005', '33333333-0000-4000-8000-000000000004', 3);

INSERT INTO crews (id, name, description, process, inputs, budget_usd) VALUES
    ('44444444-0000-4000-8000-000000000001', 'PE Due Diligence Crew', $s$Four specialist researchers each cover one diligence workstream (corporate profile, financials, safety and legal, market), then a deal partner turns their findings into an investment memo.$s$, 'sequential', '[{"key": "company", "label": "Company legal name", "default": "Northwind Holdings LLC", "required": true}, {"key": "location", "label": "Where it is based (helps tell apart same-name companies)", "default": "", "required": false}]', 2.0),
    ('44444444-0000-4000-8000-000000000002', 'AI Research Crew', $s$A research analyst uncovers the latest developments in AI and compiles a report.$s$, 'sequential', '[]', 2.0);

INSERT INTO crew_agents (crew_id, agent_id, position) VALUES
    ('44444444-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000001', 0),
    ('44444444-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000002', 1),
    ('44444444-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000003', 2),
    ('44444444-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000004', 3),
    ('44444444-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000005', 4),
    ('44444444-0000-4000-8000-000000000002', '22222222-0000-4000-8000-000000000006', 0);

INSERT INTO crew_tasks (crew_id, task_id, position) VALUES
    ('44444444-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000001', 0),
    ('44444444-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000002', 1),
    ('44444444-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000003', 2),
    ('44444444-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000004', 3),
    ('44444444-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000005', 4),
    ('44444444-0000-4000-8000-000000000002', '33333333-0000-4000-8000-000000000006', 0),
    ('44444444-0000-4000-8000-000000000002', '33333333-0000-4000-8000-000000000007', 1);

UPDATE settings SET default_budget_usd = 2.0, hard_stop_multiplier = 1.25, search_model = 'gemini-2.5-flash',
    grounded_search_cost_per_1000 = 35.0, grounded_searches_free_per_day = 1500;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Removes the imported crews, tasks, agents, LLMs and prices, with any agents or tasks you added to these two crews'
-- line-ups. Fails (and changes nothing) if these crews have runs, which would otherwise lose their crew, or if
-- your own tasks, crews or agents use the imported tasks, agents or LLMs.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runs WHERE crew_id IN ('44444444-0000-4000-8000-000000000001', '44444444-0000-4000-8000-000000000002')) THEN
        RAISE EXCEPTION 'the imported crews have runs; delete those runs first to roll this migration back';
    END IF;
END
$$;
DELETE FROM crews WHERE id IN ('44444444-0000-4000-8000-000000000001', '44444444-0000-4000-8000-000000000002');
-- The memo task takes the research tasks as context, which would stop them being deleted
DELETE FROM task_context WHERE task_id::text LIKE '33333333-0000-4000-8000-%';
DELETE FROM tasks WHERE id IN (
    '33333333-0000-4000-8000-000000000001', '33333333-0000-4000-8000-000000000002', '33333333-0000-4000-8000-000000000003',
    '33333333-0000-4000-8000-000000000004', '33333333-0000-4000-8000-000000000005', '33333333-0000-4000-8000-000000000006',
    '33333333-0000-4000-8000-000000000007');
DELETE FROM agents WHERE id IN (
    '22222222-0000-4000-8000-000000000001', '22222222-0000-4000-8000-000000000002', '22222222-0000-4000-8000-000000000003',
    '22222222-0000-4000-8000-000000000004', '22222222-0000-4000-8000-000000000005', '22222222-0000-4000-8000-000000000006');
DELETE FROM llms WHERE id IN (
    '11111111-0000-4000-8000-000000000001', '11111111-0000-4000-8000-000000000002', '11111111-0000-4000-8000-000000000003');
DELETE FROM model_prices WHERE model IN ('gpt-6-luna', 'gemini-2.5-flash');
-- The settings this migration set are the column defaults from 0001, so there is nothing to put back.
-- +goose StatementEnd
