#!/bin/bash

# Ceartax Installer - install.sh
# Kredit: Mr. Front-X
# Support: Debian/Ubuntu, Fedora/RHEL, Arch, macOS, Termux
# Fungsi: Install Go, dependensi, compile Ceartax, buat symlink

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+] $1${NC}"; }
warn() { echo -e "${YELLOW}[!] $1${NC}"; }
err() { echo -e "${RED}[-] $1${NC}"; exit 1; }

# Deteksi OS
detect_os() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        OS="macos"
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        if command -v apt >/dev/null 2>&1; then
            OS="debian"
        elif command -v dnf >/dev/null 2>&1; then
            OS="fedora"
        elif command -v pacman >/dev/null 2>&1; then
            OS="arch"
        elif [[ -f /data/data/com.termux/files/usr/bin/pkg ]]; then
            OS="termux"
        else
            err "OS tidak didukung."
        fi
    else
        err "OS tidak didukung."
    fi
}

# Install Go
install_go() {
    if command -v go >/dev/null 2>&1; then
        GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
        if [[ $(echo "$GO_VER >= 1.21" | bc -l 2>/dev/null || echo 0) -eq 1 ]]; then
            log "Go $GO_VER sudah terinstall."
            return
        fi
    fi

    log "Install Go..."
    case $OS in
        debian|ubuntu)
            sudo apt update && sudo apt install -y golang
            ;;
        fedora|rhel)
            sudo dnf install -y golang
            ;;
        arch)
            sudo pacman -S --noconfirm go
            ;;
        macos)
            brew install go
            ;;
        termux)
            pkg install -y golang
            ;;
    esac
}

# Install dependensi
install_deps() {
    log "Install dependensi..."
    case $OS in
        debian|ubuntu)
            sudo apt update
            sudo apt install -y git curl build-essential
            ;;
        fedora|rhel)
            sudo dnf install -y git curl gcc
            ;;
        arch)
            sudo pacman -S --noconfirm git curl base-devel
            ;;
        macos)
            brew install git curl
            ;;
        termux)
            pkg install -y git curl
            ;;
    esac
}

# Clone & compile
build_ceartax() {
    log "Clone & compile Ceartax..."
    if [[ ! -d "Ceartax" ]]; then
        git clone https://github.com/Front-X/Ceartax.git || err "Gagal clone repo."
        cd Ceartax
    else
        cd Ceartax
        git pull
    fi

    go mod tidy
    go build -ldflags="-s -w" -o ceartax main.go

    # Symlink ke /usr/local/bin atau $PREFIX/bin
    if [[ $OS == "termux" ]]; then
        sudo cp ceartax $PREFIX/bin/
    else
        sudo cp ceartax /usr/local/bin/
    fi
    log "Ceartax terinstall di sistem."
}

# Buat contoh UA file
create_ua_file() {
    if [[ ! -f "useragents.txt" ]]; then
        cat > useragents.txt << 'EOF'
Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36
Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15
Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36
EOF
        log "useragents.txt dibuat."
    fi
}

# Cleanup
cleanup() {
    cd ..
    rm -rf Ceartax
}

# Main
main() {
    detect_os
    install_deps
    install_go
    build_ceartax
    create_ua_file
    cleanup

    log "Instalasi selesai!"
    echo
    echo "Gunakan: ceartax -url example.com -ua-file useragents.txt"
    echo "Contoh:  ceartax -url google.com -ua-file useragents.txt -output result.json"
}

main
