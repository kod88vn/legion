> Note (2026-11-14 redesign): the .github agents, prompts and skills are superseded mirrors of the old setup; the current ones are in .claude/ and tools/puppet/.

Read and follow [AGENTS.md](../AGENTS.md): it explains this AI-Lab Ragnarok world, the Legion bot engine, and how agents act as the players' minds through `python tools/legionctl.py`.

To bring the world to life, follow the skill in `.github/skills/legion-session/SKILL.md` (or run the `legion-start` prompt). Role skills: legion-chat, legion-life, legion-guild, legion-market, legion-ops (all under `.github/skills`). Custom agents for the roles are in `.github/agents`.
