package hostfiles

import "os"

// The deployment directory's layout: where each secret lives, who owns it, and which
// env key points the container at it. Quoted lines are the source of each value.
var (
	// server/Dockerfile: `useradd --system --uid 10001 ... dashboard`; both the app and
	// the backup sidecar run as this user. fresh-install.md §5: "App and backup files
	// need owner UID 10001 and mode 0400".
	AppIDs = IDs{10001, 10001}
	// compose.cloudflare.yml, cloudflared: `user: "65532:65532"`. fresh-install.md §5:
	// "the tunnel token file needs UID 65532 and mode 0400".
	TunnelIDs = IDs{65532, 65532}
)

// Dirs are the secret directories, parents first.
var Dirs = []DirSpec{
	// fresh-install.md §5: "The root-owned `secrets` parent must permit directory
	// traversal by UID 65532." and "chmod 0711 secrets".
	{Path: "secrets", Mode: 0o711, Owner: Root},
	// fresh-install.md §5: "install -d -m 0700 -o 10001 -g 10001 secrets/app secrets/backup"
	{Path: "secrets/app", Mode: 0o700, Owner: AppIDs},
	{Path: "secrets/backup", Mode: 0o700, Owner: AppIDs},
}

// AdminDir holds secrets that no container reads (such as the services token's secret,
// which belongs in the bots and agents that call the dashboard). Only root can open it.
var AdminDir = DirSpec{Path: "secrets/admin", Mode: 0o700, Owner: Root}

// DirSpec is one directory with its mode and owner.
type DirSpec struct {
	Path  string
	Mode  os.FileMode
	Owner IDs
}

// SecretSpec is one named secret: its host file and the env key that names the file's
// path inside the container.
type SecretSpec struct {
	Name     string
	Path     string // relative to the deploy directory
	Owner    IDs
	EnvFile  string // the env file holding EnvKey
	EnvKey   string
	EnvValue string // the path the container sees (Compose bind mount target)
	Optional bool   // the install works without it
}

// Secrets lists every secret setup knows. The table in fresh-install.md §5 and the
// *_FILE lines of the env templates name the files; compose.cloudflare.yml mounts
// ./secrets/app at /run/secrets/app, ./secrets/backup at /run/secrets/backup, and
// TUNNEL_TOKEN_FILE (a host path, relative to the deploy directory) at
// /run/secrets/tunnel-token.
var Secrets = []SecretSpec{
	{Name: "tunnel-token", Path: "secrets/tunnel-token", Owner: TunnelIDs, EnvFile: ".env", EnvKey: "TUNNEL_TOKEN_FILE", EnvValue: "./secrets/tunnel-token"},
	{Name: "discord-client-secret", Path: "secrets/app/discord-client-secret", Owner: AppIDs, EnvFile: "dashboard.env", EnvKey: "DISCORD_CLIENT_SECRET_FILE", EnvValue: "/run/secrets/app/discord-client-secret"},
	{Name: "discord-webhook-url", Path: "secrets/app/discord-webhook-url", Owner: AppIDs, EnvFile: "dashboard.env", EnvKey: "DISCORD_WEBHOOK_URL_FILE", EnvValue: "/run/secrets/app/discord-webhook-url", Optional: true},
	{Name: "discord-bot-token", Path: "secrets/app/discord-bot-token", Owner: AppIDs, EnvFile: "dashboard.env", EnvKey: "DISCORD_BOT_TOKEN_FILE", EnvValue: "/run/secrets/app/discord-bot-token", Optional: true},
	{Name: "backup-bearer", Path: "secrets/backup/bearer", Owner: AppIDs, EnvFile: "backup.env", EnvKey: "BACKUP_BEARER_SECRET_FILE", EnvValue: "/run/secrets/backup/bearer"},
	{Name: "backup-access-client-id", Path: "secrets/backup/access-client-id", Owner: AppIDs, EnvFile: "backup.env", EnvKey: "BACKUP_ACCESS_CLIENT_ID_FILE", EnvValue: "/run/secrets/backup/access-client-id"},
	{Name: "backup-access-client-secret", Path: "secrets/backup/access-client-secret", Owner: AppIDs, EnvFile: "backup.env", EnvKey: "BACKUP_ACCESS_CLIENT_SECRET_FILE", EnvValue: "/run/secrets/backup/access-client-secret"},
}

// LookupSecret finds a secret by name.
func LookupSecret(name string) (SecretSpec, bool) {
	for _, s := range Secrets {
		if s.Name == name {
			return s, true
		}
	}
	return SecretSpec{}, false
}
