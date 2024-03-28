#!/bin/sh

set -e

# Base URL for git-hound releases
RELEASES_URL="https://github.com/tillson/git-hound/releases"
FILE_BASENAME="git-hound"

# Determine system architecture
ARCHITECTURE="$(uname -m)"
case "$ARCHITECTURE" in
    x86_64)
        ARCHITECTURE="amd64"
        ;;
    i386|i686)
        ARCHITECTURE="386"
        ;;
    aarch64)
        ARCHITECTURE="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCHITECTURE. git-hound may not provide a pre-compiled binary for this architecture."
        exit 1
        ;;
esac

OS_NAME="linux" # Assuming the script is intended for Linux; adjust as necessary for other OSes

# Fetch the latest git-hound version
VERSION="$(curl -sfL -o /dev/null -w %{url_effective} "$RELEASES_URL/latest" | rev | cut -f1 -d'/' | rev)"
VERSION="${VERSION#v}"

if [ -z "$VERSION" ]; then
    echo "Unable to determine the latest git-hound version." >&2
    exit 1
fi

echo "Latest git-hound version: $VERSION"

# Construct the download URL
DOWNLOAD_URL="$RELEASES_URL/download/v$VERSION/${FILE_BASENAME}_${VERSION}_${OS_NAME}_${ARCHITECTURE}.tar.gz"

TMPDIR="$(mktemp -d)"
TAR_FILE="$TMPDIR/${FILE_BASENAME}.tar.gz"

echo "Downloading git-hound $VERSION for $ARCHITECTURE..."
curl -sfLo "$TAR_FILE" "$DOWNLOAD_URL"

# Extract the tar.gz file
tar -xzf "$TAR_FILE" -C "$TMPDIR"

# Move the binary to /usr/bin (or another directory in your PATH)
sudo mv "$TMPDIR/${FILE_BASENAME}" /usr/bin/


# Setup git-hound configuration
CONFIG_DIR="$HOME/.githound"
mkdir -p "$CONFIG_DIR"
CONFIG_FILE="$CONFIG_DIR/config.yml"

echo "Setting up git-hound configuration..."
read -p "Enter your GitHub username (press enter to skip): " github_username

if [ -n "$github_username" ]; then
    read -sp "Enter your GitHub password (press enter to skip): " github_password
    echo ""
    if [ -z "$github_password" ]; then
        echo "Since you've skipped entering the password, default values will be used for both username and password."
        github_username="username"
        github_password="your_password"
    fi
else
    echo "You've skipped entering the username. Default values will be used for both username and password."
    github_username="username"
    github_password="your_password"
fi

# Create or update the config.yml file
# This is the config.example.yml in the repository.
{
    echo "# DO NOT CHECK YOUR USERNAME AND PASSWORD INTO GIT!"
    echo ""
    echo "# Required"
    echo "github_username: \"$github_username\""
    echo "github_password: \"$github_password\""
    echo ""
    echo "# Optional (comment out if not using)"
    echo "# github_totp_seed: \"ABCDEF1234567890\" # Obtained via https://github.com/settings/security"
} > "$CONFIG_FILE"

echo "git-hound configuration has been set."

# Cleanup
rm -rf "$TMPDIR"

echo "git-hound has been downloaded and installed successfully."
