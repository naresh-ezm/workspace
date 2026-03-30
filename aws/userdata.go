package awsclient

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// validLinuxUsername matches lowercase letters, digits, hyphens, and underscores,
// starting with a letter, max 32 characters — safe for shell embedding.
var validLinuxUsername = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// BuildSetupScript returns a base64-encoded EC2 user-data shell script that:
//   - Creates the developer user account with devPassword
//   - Locks dnsmasq / DNS config files behind a dedicated "dnsguard" account
//     whose password is guardPassword (the "special password", independent of the dev user)
//   - Sets authorized_keys immutable so the dev user cannot add SSH keys
//   - Blocks the dev user from stopping or disabling the xrdp service via polkit
func BuildSetupScript(devUsername, devPassword, guardPassword string) (string, error) {
	if !validLinuxUsername.MatchString(devUsername) {
		return "", fmt.Errorf(
			"invalid username %q: must be lowercase letters/digits/hyphens/underscores, start with a letter, max 32 chars",
			devUsername,
		)
	}
	if devPassword == "" {
		return "", fmt.Errorf("devPassword must not be empty")
	}
	if guardPassword == "" {
		return "", fmt.Errorf("guardPassword must not be empty")
	}
	script := setupScriptTemplate
	script = strings.ReplaceAll(script, "___DEV_USER___", devUsername)
	script = strings.ReplaceAll(script, "___DEV_PASS___", devPassword)
	script = strings.ReplaceAll(script, "___GUARD_PASS___", guardPassword)
	return base64.StdEncoding.EncodeToString([]byte(script)), nil
}

// setupScriptTemplate is a bash user-data script run as root on first boot.
// ___DEV_USER___, ___DEV_PASS___, and ___GUARD_PASS___ are replaced before encoding.
const setupScriptTemplate = `#!/bin/bash
set -euo pipefail
exec >> /var/log/workspace-setup.log 2>&1
echo "=== workspace setup starting $(date) ==="

DEV_USER="___DEV_USER___"
DEV_PASS="___DEV_PASS___"
GUARD_PASS="___GUARD_PASS___"

# ── 1. Create the developer user ──────────────────────────────────────────────
if ! id "$DEV_USER" &>/dev/null; then
    useradd -m -s /bin/bash "$DEV_USER"
    echo "Created user $DEV_USER."
fi
echo "$DEV_USER:$DEV_PASS" | chpasswd
echo "Password set for $DEV_USER."

# ── 2. Restrict dnsmasq and DNS configuration files ──────────────────────────
# A dedicated "dnsguard" system account owns the dnsmasq configuration files.
# Its password is the "special password" required to modify those files;
# it is completely independent of the developer user's password.
if ! id dnsguard &>/dev/null; then
    useradd -r -M -s /usr/sbin/nologin \
        -c "DNS configuration guardian" dnsguard
fi
echo "dnsguard:$GUARD_PASS" | chpasswd
echo "dnsguard account configured."

# Transfer ownership of dnsmasq config to dnsguard.
# The dnsmasq daemon runs as root so it can still read these files, but the
# developer user has no read or write access.
for cfg_path in /etc/dnsmasq.conf /etc/dnsmasq.d; do
    [ -e "$cfg_path" ] && chown -R dnsguard:dnsguard "$cfg_path"
done
[ -f /etc/dnsmasq.conf ] && chmod 640 /etc/dnsmasq.conf
[ -d /etc/dnsmasq.d   ] && chmod 750 /etc/dnsmasq.d

# Polkit rule: deny the developer from managing the dnsmasq service.
mkdir -p /etc/polkit-1/rules.d
cat > /etc/polkit-1/rules.d/50-dnsmasq-guard.rules << EOF
polkit.addRule(function(action, subject) {
    var controlled = [
        "org.freedesktop.systemd1.manage-units",
        "org.freedesktop.systemd1.manage-unit-files"
    ];
    if (controlled.indexOf(action.id) !== -1 &&
        action.lookup("unit") !== undefined &&
        action.lookup("unit").indexOf("dnsmasq") !== -1 &&
        subject.user === "$DEV_USER") {
        return polkit.Result.NO;
    }
});
EOF

echo "dnsmasq restrictions applied."

# ── 3. Lock authorized_keys – prevent dev user from adding SSH keys ───────────
SSH_DIR="/home/$DEV_USER/.ssh"
mkdir -p "$SSH_DIR"
touch "$SSH_DIR/authorized_keys"
chown -R "$DEV_USER:$DEV_USER" "$SSH_DIR"
chmod 700 "$SSH_DIR"
chmod 600 "$SSH_DIR/authorized_keys"

# Make authorized_keys immutable so even the owning user cannot modify it.
# Root can lift this flag with: chattr -i /home/<user>/.ssh/authorized_keys
if command -v chattr &>/dev/null; then
    chattr +i "$SSH_DIR/authorized_keys"
    echo "authorized_keys locked with immutable flag."
else
    # Fallback when e2fsprogs is not installed
    chown root:root "$SSH_DIR/authorized_keys"
    chmod 444     "$SSH_DIR/authorized_keys"
    echo "authorized_keys locked via root ownership + read-only (chattr not available)."
fi

# ── 4. Prevent dev user from stopping or disabling the xrdp service ───────────
cat > /etc/polkit-1/rules.d/50-xrdp-guard.rules << EOF
polkit.addRule(function(action, subject) {
    var controlled = [
        "org.freedesktop.systemd1.manage-units",
        "org.freedesktop.systemd1.manage-unit-files"
    ];
    if (controlled.indexOf(action.id) !== -1 &&
        action.lookup("unit") !== undefined &&
        action.lookup("unit").indexOf("xrdp") !== -1 &&
        subject.user === "$DEV_USER") {
        return polkit.Result.NO;
    }
});
EOF

echo "xrdp restrictions applied."
echo "=== workspace setup complete $(date) ==="
`
