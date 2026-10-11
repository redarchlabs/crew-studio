---
title: Questions from a crew
category: Console
order: 22
permissions: work_orders.view, crews.edit
summary: How a crew's agents stop and ask you a question during a run, how to answer, and how to set the rules for when they ask.
keywords: question, ask, ask a human, agent chat, hand-off, escalate, escalation, human input, human in the loop, answer, waiting for you, timeout, rules, feedback, badge
---
A crew can stop and ask you a question instead of guessing: "What budget should I assume?", "Two sources disagree on the price; which one do you trust?" The run waits for your answer, then carries on with it.

## Answering a question
<!-- permissions: work_orders.view -->

When an agent asks you something, **Work orders** in the menu shows a badge with the number of questions waiting for you. It updates by itself; you do not need to refresh.

1. Open the work order. A **Waiting for you** card sits above the tabs, with the question, which agent asked it and why.
2. Type your answer and click **Send answer**. The agent gets it straight away and the run carries on.

Questions go to the person who started the run. When a schedule, trigger or service account started it, anyone who can cancel work orders in its areas can answer.

Every question has a deadline. Without an answer by then, the crew follows its rules: either the agent carries on and says in its output what it assumed, or the run stops. Earlier questions and their answers stay on the work order under **Earlier questions**, and each one is also a line in the journal.

## Questions from an agent chat
<!-- permissions: crews.chat -->
A hand-off in an agent chat can ask you too, when the agent's builder allows it. Those questions wait in the conversation, under the hand-off, not on a work order: the badge for them is on **Chat**, not on **Work orders**. See [Questions during a hand-off](/help/chat#questions-during-a-hand-off).

## Setting when a crew asks
<!-- permissions: crews.edit -->

Open the crew and find **Human input**.

- **Agents may ask a person** turns asking on.
- **When to ask** is plain language, one rule per line. For example: "Ask before assuming a budget, deadline or region the brief does not give." Every agent of the crew gets these rules; outside them, agents decide for themselves.
- **Wait for an answer** is how long a question waits, from 1 to 180 minutes.
- **If nobody answers**: **Carry on and state the assumption**, or **Stop the run**.
- **Questions per run** stops a looping agent from flooding you with questions.

Changes apply to runs that start after you save. Runs already going keep the rules they started with.
