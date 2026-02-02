#!/usr/bin/env bash
#
# bdh (BeadHub CLI) installation script
# Usage: curl -fsSL https://raw.githubusercontent.com/beadhub/bdh/main/install.sh | bash
#
# Security note: For maximum security, download and inspect the script first:
#   curl -fsSL https://raw.githubusercontent.com/beadhub/bdh/main/install.sh > install.sh
#   less install.sh  # Review the script
#   bash install.sh
#
# IMPORTANT: This script must be EXECUTED, never SOURCED
# WRONG: source install.sh (will exit your shell on errors)
# CORRECT: bash install.sh
# CORRECT: curl -fsSL ... | bash

set -e

REPO="beadhub/bdh"
BINARY="bdh"

# Track where we installed for PATH warning messages
LAST_INSTALL_PATH=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}==>${NC} $1"
}

log_success() {
    echo -e "${GREEN}==>${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}==>${NC} $1"
}

log_error() {
    echo -e "${RED}Error:${NC} $1" >&2
}

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Darwin)
            os="darwin"
            ;;
        Linux)
            os="linux"
            ;;
        FreeBSD)
            os="freebsd"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            os="windows"
            ;;
        *)
            log_error "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        armv7*|armv6*|armhf|arm)
            arch="arm"
            ;;
        *)
            log_error "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac

    echo "${os}_${arch}"
}

# Re-sign binary for macOS to avoid slow Gatekeeper checks
resign_for_macos() {
    local binary_path=$1

    # Only run on macOS
    if [[ "$(uname -s)" != "Darwin" ]]; then
        return 0
    fi

    # Check if codesign is available
    if ! command -v codesign >/dev/null 2>&1; then
        log_warning "codesign not found, skipping re-signing"
        return 0
    fi

    log_info "Re-signing binary for macOS..."
    codesign --remove-signature "$binary_path" 2>/dev/null || true
    if codesign --force --sign - "$binary_path"; then
        log_success "Binary re-signed for this machine"
    else
        log_warning "Failed to re-sign binary (non-fatal)"
    fi
}

# Get the latest release version
get_latest_version() {
    local response

    log_info "Fetching latest release..."

    if command -v curl >/dev/null 2>&1; then
        response=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")
    elif command -v wget >/dev/null 2>&1; then
        response=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest")
    else
        log_error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi

    # Store full response for asset checking
    RELEASE_JSON="$response"

    # Extract version from tag_name field (vX.Y.Z -> X.Y.Z)
    VERSION=$(echo "$response" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/' | head -1)

    if [ -z "$VERSION" ]; then
        log_error "Could not determine latest version"
        exit 1
    fi

    log_info "Latest version: v$VERSION"
}

# Check if release has a specific asset
release_has_asset() {
    local asset_name=$1

    if echo "$RELEASE_JSON" | grep -Fq "\"name\": \"$asset_name\""; then
        return 0
    fi

    return 1
}

# Verify checksum of downloaded file
verify_checksum() {
    local file="$1"
    local checksums_file="$2"
    local filename
    filename=$(basename "$file")
    local expected actual

    log_info "Verifying checksum..."

    if command -v sha256sum >/dev/null 2>&1; then
        expected=$(grep "$filename" "$checksums_file" | awk '{print $1}')
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        expected=$(grep "$filename" "$checksums_file" | awk '{print $1}')
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        log_error "sha256sum or shasum required for checksum verification"
        echo "Install coreutils (Linux) or use macOS which includes shasum"
        exit 1
    fi

    if [ "$expected" != "$actual" ]; then
        log_error "Checksum verification failed!"
        echo "  Expected: $expected"
        echo "  Actual:   $actual"
        exit 1
    fi

    log_success "Checksum verified"
}

# Returns a list of full paths to 'bdh' found in PATH (earlier entries first)
get_bdh_paths_in_path() {
    local IFS=':'
    local -a entries
    read -ra entries <<< "$PATH"
    local -a found
    local p
    for p in "${entries[@]}"; do
        [ -z "$p" ] && continue
        if [ -x "$p/bdh" ]; then
            # Resolve symlink if possible
            local resolved
            if command -v readlink >/dev/null 2>&1; then
                resolved=$(readlink -f "$p/bdh" 2>/dev/null || printf '%s' "$p/bdh")
            else
                resolved="$p/bdh"
            fi
            # avoid duplicates
            local skip=0
            local existing
            for existing in "${found[@]:-}"; do
                if [ "$existing" = "$resolved" ]; then skip=1; break; fi
            done
            if [ $skip -eq 0 ]; then
                found+=("$resolved")
            fi
        fi
    done
    # print results, one per line
    local item
    for item in "${found[@]:-}"; do
        printf '%s\n' "$item"
    done
}

warn_if_multiple_bdh() {
    # Bash 3.2-compatible approach (no mapfile, no process substitution)
    local paths_output
    paths_output=$(get_bdh_paths_in_path)
    local -a bdh_paths=()
    while IFS= read -r line; do
        [ -n "$line" ] && bdh_paths+=("$line")
    done <<< "$paths_output"

    if [ "${#bdh_paths[@]}" -le 1 ]; then
        return 0
    fi

    log_warning "Multiple 'bdh' executables found on your PATH. An older copy may be executed instead of the one we installed."
    echo "Found the following 'bdh' executables (entries earlier in PATH take precedence):"
    local i=1
    local p
    for p in "${bdh_paths[@]}"; do
        local ver
        if [ -x "$p" ]; then
            ver=$("$p" version 2>/dev/null || true)
        fi
        if [ -z "$ver" ]; then ver="<unknown version>"; fi
        echo "  $i. $p  -> $ver"
        i=$((i+1))
    done

    if [ -n "$LAST_INSTALL_PATH" ]; then
        echo ""
        echo "We installed to: $LAST_INSTALL_PATH"
        # Compare first PATH entry vs installed path
        local first="${bdh_paths[0]}"
        if [ "$first" != "$LAST_INSTALL_PATH" ]; then
            log_warning "The 'bdh' executable that appears first in your PATH is different from the one we installed."
            echo "To make the newly installed 'bdh' the one you get when running 'bdh', either:"
            echo "  - Remove or rename the older $first from your PATH, or"
            echo "  - Reorder your PATH so that $(dirname "$LAST_INSTALL_PATH") appears before $(dirname "$first")"
            echo "After updating PATH, restart your shell and run 'bdh version' to confirm."
        else
            echo "The installed 'bdh' is first in your PATH."
        fi
    else
        log_warning "We couldn't determine where we installed 'bdh' during this run."
    fi
}

# Verify installation
verify_installation() {
    # If multiple 'bdh' binaries exist on PATH, warn the user before verification
    warn_if_multiple_bdh || true

    if command -v bdh >/dev/null 2>&1; then
        log_success "bdh is installed and ready!"
        echo ""
        bdh version 2>/dev/null || echo "bdh (development build)"
        echo ""
        echo "Get started:"
        echo "  cd your-project"
        echo "  bdh :init              # Register workspace with BeadHub"
        echo "  bdh :status            # Show coordination status"
        echo "  bdh ready              # Show ready work"
        echo ""
        return 0
    else
        log_error "bdh was installed but is not in PATH"
        return 1
    fi
}

# Download and install from GitHub releases
install_from_release() {
    local platform=$1
    local tmp_dir
    tmp_dir=$(mktemp -d)

    # Get latest release version
    get_latest_version

    # Construct download URL
    local ext="tar.gz"
    local binary="bdh"
    if [[ "$platform" == windows_* ]]; then
        ext="zip"
        binary="bdh.exe"
    fi

    local archive_name="${binary}_${VERSION}_${platform}.${ext}"
    local url="https://github.com/$REPO/releases/download/v$VERSION/$archive_name"
    local checksums_url="https://github.com/$REPO/releases/download/v$VERSION/checksums.txt"

    if ! release_has_asset "$archive_name"; then
        log_warning "No prebuilt archive available for platform ${platform}."
        rm -rf "$tmp_dir"
        return 1
    fi

    # Check if checksums are available
    local skip_checksum=""
    if ! release_has_asset "checksums.txt"; then
        log_warning "Checksums not available for this release, skipping verification"
        skip_checksum=1
    fi

    log_info "Downloading $archive_name..."

    cd "$tmp_dir"
    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL -o "$archive_name" "$url"; then
            log_error "Download failed"
            cd - > /dev/null || cd "$HOME"
            rm -rf "$tmp_dir"
            return 1
        fi
        if [ -z "$skip_checksum" ]; then
            if ! curl -fsSL -o "checksums.txt" "$checksums_url"; then
                log_warning "Failed to download checksums, skipping verification"
                skip_checksum=1
            fi
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -q -O "$archive_name" "$url"; then
            log_error "Download failed"
            cd - > /dev/null || cd "$HOME"
            rm -rf "$tmp_dir"
            return 1
        fi
        if [ -z "$skip_checksum" ]; then
            if ! wget -q -O "checksums.txt" "$checksums_url"; then
                log_warning "Failed to download checksums, skipping verification"
                skip_checksum=1
            fi
        fi
    fi

    # Verify checksum if available
    if [ -z "$skip_checksum" ]; then
        verify_checksum "$tmp_dir/$archive_name" "$tmp_dir/checksums.txt"
    fi

    # Extract archive
    log_info "Extracting archive..."
    if [ "$ext" = "tar.gz" ]; then
        if ! tar -xzf "$archive_name"; then
            log_error "Failed to extract archive"
            rm -rf "$tmp_dir"
            return 1
        fi
    else
        if ! command -v unzip >/dev/null 2>&1; then
            log_error "unzip required but not found"
            rm -rf "$tmp_dir"
            return 1
        fi
        if ! unzip -q "$archive_name"; then
            log_error "Failed to extract archive"
            rm -rf "$tmp_dir"
            return 1
        fi
    fi

    # Determine install location
    local install_dir
    if [[ -w /usr/local/bin ]]; then
        install_dir="/usr/local/bin"
    else
        install_dir="$HOME/.local/bin"
        mkdir -p "$install_dir"
    fi

    # Install binary
    log_info "Installing to $install_dir..."
    if [[ -w "$install_dir" ]]; then
        mv "$binary" "$install_dir/"
        chmod +x "$install_dir/$binary"
    else
        sudo mv "$binary" "$install_dir/"
        sudo chmod +x "$install_dir/$binary"
    fi

    # Re-sign for macOS to avoid Gatekeeper delays
    resign_for_macos "$install_dir/$binary"

    LAST_INSTALL_PATH="$install_dir/$binary"
    log_success "bdh installed to $install_dir/$binary"

    # Check if install_dir is in PATH
    if [[ ":$PATH:" != *":$install_dir:"* ]]; then
        log_warning "$install_dir is not in your PATH"
        echo ""
        echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo "  export PATH=\"\$PATH:$install_dir\""
        echo ""
    fi

    cd - > /dev/null || cd "$HOME"
    rm -rf "$tmp_dir"
    return 0
}

# Check if Go is installed and meets minimum version
check_go() {
    if command -v go >/dev/null 2>&1; then
        local go_version
        go_version=$(go version | awk '{print $3}' | sed 's/go//')
        log_info "Go detected: $(go version)"

        # Extract major and minor version numbers
        local major minor
        major=$(echo "$go_version" | cut -d. -f1)
        minor=$(echo "$go_version" | cut -d. -f2)

        # Check if Go version is 1.21 or later
        if [ "$major" -eq 1 ] && [ "$minor" -lt 21 ]; then
            log_error "Go 1.21 or later is required (found: $go_version)"
            echo ""
            echo "Please upgrade Go:"
            echo "  - Download from https://go.dev/dl/"
            echo "  - Or use your package manager to update"
            echo ""
            return 1
        fi

        return 0
    else
        return 1
    fi
}

# Install using go install (fallback)
install_with_go() {
    log_info "Installing bdh using 'go install'..."

    if go install github.com/$REPO/cmd/bdh@latest; then
        log_success "bdh installed successfully via go install"

        # Record where we expect the binary to have been installed
        local gobin bin_dir
        gobin=$(go env GOBIN 2>/dev/null || true)
        if [ -n "$gobin" ]; then
            bin_dir="$gobin"
        else
            bin_dir="$(go env GOPATH)/bin"
        fi
        LAST_INSTALL_PATH="$bin_dir/bdh"

        # Re-sign for macOS to avoid Gatekeeper delays
        resign_for_macos "$bin_dir/bdh"

        # Check if GOPATH/bin (or GOBIN) is in PATH
        if [[ ":$PATH:" != *":$bin_dir:"* ]]; then
            log_warning "$bin_dir is not in your PATH"
            echo ""
            echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
            echo "  export PATH=\"\$PATH:$bin_dir\""
            echo ""
        fi

        return 0
    else
        log_error "go install failed"
        return 1
    fi
}

# Build from source (last resort)
build_from_source() {
    log_info "Building bdh from source..."

    local tmp_dir
    tmp_dir=$(mktemp -d)

    cd "$tmp_dir"
    log_info "Cloning repository..."

    if git clone --depth 1 https://github.com/$REPO.git; then
        cd bdh
        log_info "Building binary..."

        if go build -o bdh ./cmd/bdh; then
            # Determine install location
            local install_dir
            if [[ -w /usr/local/bin ]]; then
                install_dir="/usr/local/bin"
            else
                install_dir="$HOME/.local/bin"
                mkdir -p "$install_dir"
            fi

            log_info "Installing to $install_dir..."
            if [[ -w "$install_dir" ]]; then
                mv bdh "$install_dir/"
            else
                sudo mv bdh "$install_dir/"
            fi

            # Re-sign for macOS to avoid Gatekeeper delays
            resign_for_macos "$install_dir/bdh"

            log_success "bdh installed to $install_dir/bdh"
            LAST_INSTALL_PATH="$install_dir/bdh"

            # Check if install_dir is in PATH
            if [[ ":$PATH:" != *":$install_dir:"* ]]; then
                log_warning "$install_dir is not in your PATH"
                echo ""
                echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
                echo "  export PATH=\"\$PATH:$install_dir\""
                echo ""
            fi

            cd - > /dev/null || cd "$HOME"
            rm -rf "$tmp_dir"
            return 0
        else
            log_error "Build failed"
            cd - > /dev/null || cd "$HOME"
            rm -rf "$tmp_dir"
            return 1
        fi
    else
        log_error "Failed to clone repository"
        rm -rf "$tmp_dir"
        return 1
    fi
}

# Main installation flow
main() {
    echo ""
    echo "🔗 bdh (BeadHub CLI) Installer"
    echo ""

    log_info "Detecting platform..."
    local platform
    platform=$(detect_platform)
    log_info "Platform: $platform"

    # Try downloading from GitHub releases first
    if install_from_release "$platform"; then
        verify_installation
        exit 0
    fi

    log_warning "Failed to install from releases, trying alternative methods..."

    # Try go install as fallback
    if check_go; then
        if install_with_go; then
            verify_installation
            exit 0
        fi
    fi

    # Try building from source as last resort
    log_warning "Falling back to building from source..."

    if ! check_go; then
        log_warning "Go is not installed"
        echo ""
        echo "bdh requires Go 1.21 or later to build from source. You can:"
        echo "  1. Install Go from https://go.dev/dl/"
        echo "  2. Use your package manager:"
        echo "     - macOS: brew install go"
        echo "     - Ubuntu/Debian: sudo apt install golang"
        echo "     - Other Linux: Check your distro's package manager"
        echo ""
        echo "After installing Go, run this script again."
        exit 1
    fi

    if build_from_source; then
        verify_installation
        exit 0
    fi

    # All methods failed
    log_error "Installation failed"
    echo ""
    echo "Manual installation:"
    echo "  1. Download from https://github.com/$REPO/releases/latest"
    echo "  2. Extract and move 'bdh' to your PATH"
    echo ""
    echo "Or install from source:"
    echo "  1. Install Go from https://go.dev/dl/"
    echo "  2. Run: go install github.com/$REPO/cmd/bdh@latest"
    echo ""
    exit 1
}

main "$@"
