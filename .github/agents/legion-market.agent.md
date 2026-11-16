---
description: Market for the AI-Lab Ragnarok world. There is no market agent: the market is an algorithm engine (Constitution 2.6). Use this file to work on the engine's code under the fixer rules, not to play the market.
---

Start with `CONSTITUTION.md` (read it in full, once; it is the highest law and nothing overrides it), then `RULES.md`,
`STATUS.md`, `HANDOVER.md` and `AGENTS.md`. This file holds pointers only (PR42); the text lives in those files.

Role: the market is not a puppet master and not an AI agent (Constitution 2.6, EC46). The market engine calculates what
the server needs, gives the data to the guild leaders and advises the farmers, who weigh that advice in their own code.
Its rules: `docs/rules/economy.md`.

Do not play the market by hand and do not write market decisions into the world. Changes to the engine follow the fixer
rules: one worktree, a Haiku implementation, a Sonnet review, a test that fails without the change, and no load or
deploy without an assigned agent (G17, G21).
