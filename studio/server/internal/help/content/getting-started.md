---
title: Getting started
category: Getting started
order: 10
summary: How to sign in, find your way around Crew Studio, switch between light and dark mode, use Help and sign out.
keywords: sign in, log in, login, single sign-on, SSO, Microsoft, Entra, Clerk, password, administrator sign-in, session, sidebar, menu, navigation, dark mode, light mode, theme, sign out, log out, help, search, missing page, no access
---
Crew Studio is where your organisation designs, runs and inspects AI crews: groups of agents that work through tasks. This article covers the basics that apply to everyone: signing in, moving around, and finding help.

## Signing in

When you open Crew Studio without an active session, you land on the **Sign in** page.

![The Sign in page with a Continue with Microsoft button and the folded Administrator sign-in link.](login.png)

1. Click **Continue with Microsoft** or **Continue with Clerk** (you see whichever your organisation has set up).
2. Sign in with your usual work account at the provider.
3. You are sent back to Crew Studio, to the page you were trying to open.

You need to have been invited before you can sign in, unless your organisation lets new people in automatically. If something goes wrong, the Sign in page tells you why:

| Message | What to do |
|---|---|
| Your account hasn't been set up yet | Ask an administrator to invite you. |
| Your account has been disabled | Ask an administrator if you think this is a mistake. |
| Your sign-in provider has not verified your email address | Verify your email with Microsoft or Clerk, then try again. |
| The sign-in took too long and expired | Try again. |

### Administrator sign-in

Below the sign-in buttons there is a folded **Administrator sign-in** link. It opens an **Email** and **Password** form that only administrator accounts can use, for example during first-time setup or when single sign-on is not working. Everyone else should use the buttons above it. After five wrong passwords the account is locked for 15 minutes.

### How long you stay signed in

A session lasts up to 7 days, and ends sooner if you do not use Crew Studio for 12 hours. When a session ends you are taken back to the Sign in page, and after signing in again you return to where you were.

## Finding your way around

The sidebar on the left lists the screens you can use, grouped into sections:

- **Console**: Overview, Work orders and Runs, for asking crews to do work and following what they did.
- **Build**: Crews, Agents and Tasks, where crews are designed.
- **Automate**: Schedules and Triggers, which start work automatically.
- **Configure**: LLMs, Pricing, Settings and History.
- **Developers**: the API explorer.
- **Admin**: people, roles, functional areas, sign-in settings and the audit log.

Most people see only some of these. A section with nothing in it for you is left out entirely.

On a phone or narrow window the sidebar is hidden. Tap the menu button at the top left to open it, and tap a screen name (or the close button) to put it away.

## Light and dark mode

Crew Studio starts in dark mode. To switch, click **Light mode** (or **Dark mode**) at the bottom of the sidebar. Your choice is remembered in this browser.

## Getting help

The Help page holds articles like this one. It shows only the articles, and the parts of articles, that match what you can do in Crew Studio, so the instructions you read always apply to you.

![The Help page with the table of contents on the left and an article open on the right.](help.png)

- **Table of contents**: articles are grouped by category. Click a title to open the article.
- **Search**: type a question or a few words. Search looks at what you mean, not only the exact words, so "why can't I see the crews page" finds the article about access even though it never uses that sentence.

## Why you might not see a screen

What you see depends on three things your administrator sets for you:

- **Permissions**: each screen and each action (create, edit, delete, cancel and so on) needs a specific permission.
- **Roles**: bundles of permissions, such as Viewer or Builder.
- **Functional areas**: groups such as Finance or IT. You only see crews, agents, tasks, work orders and runs that belong to your areas.

If you open a page you do not have permission for, you see "You don't have access to this page" with the permission it needs. Items outside your areas do not appear at all. [Access, roles and functional areas](/help/access-and-areas) explains this in more detail and tells you who to ask.

## Signing out

Your name and email appear at the bottom of the sidebar. Click the sign-out button next to them to end your session. You are taken back to the Sign in page. On a shared computer, also sign out of your Microsoft or Clerk account if you want to stop others signing in as you.
