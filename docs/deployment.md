# Deployment

This page moved. To deploy, start at **[Deploy the dashboard, step by step](deploy/README.md)**.

| You want | Go to |
| --- | --- |
| a first deployment (Docker, Cloudflare, backups, Google) | [Deploy the dashboard, step by step](deploy/README.md) |
| the Cloudflare setup (tunnel, Access, bot settings, R2) | [Cloudflare setup](deploy/cloudflare.md) |
| to upgrade, roll back, move to a new machine or publish a release | [Upgrade](deploy/upgrade.md) |
| the org Google calendar | [Google calendar setup](deploy/google-calendar.md) |
| a fix for an error | [Troubleshooting](deploy/troubleshooting.md) |
| a Pi under systemd, a self-built image, a quick-tunnel practice run, or what's in the image | [Other setups](deploy/other-setups.md) |
| every setting and its default | [Configuration](configuration.md) |

## Cloudflare Tunnel + Access

`dashboard mcp-check` and some older pages name "docs/deployment.md, Cloudflare Tunnel + Access,
step N". The step numbers are unchanged in [Cloudflare setup](deploy/cloudflare.md):

| Step | Now |
| --- | --- |
| 1 | [Create the tunnel](deploy/cloudflare.md#1-create-the-tunnel) |
| 2 | [One-time PIN and the groups](deploy/cloudflare.md#2-turn-on-one-time-pin-and-create-the-groups) |
| 3 | [The Access application](deploy/cloudflare.md#3-protect-the-whole-site-with-an-access-application) |
| 4 | [Agent paths bypass Access](deploy/cloudflare.md#4-let-ai-agents-through-on-their-own-paths) |
| 5 | [The service token](deploy/cloudflare.md#5-create-a-service-token-for-bots) |
| 6 | [Bot Fight Mode, Browser Integrity Check, WAF](deploy/cloudflare.md#6-turn-off-bot-checks-for-agents) |
| 7 | [Cache rule](deploy/cloudflare.md#7-never-cache-the-agent-paths) |
| 8 | [Rocket Loader and Email Obfuscation](deploy/cloudflare.md#8-turn-off-rocket-loader-and-email-obfuscation) |
| 9 (check from outside) | [Check it from outside](deploy/cloudflare.md#10-check-it-from-outside), now step 10 |
