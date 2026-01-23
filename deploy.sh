#!/bin/bash

# Deployment script for nofx project
# Supports deployment to both API (qisg) and Web (qibj) servers

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to prompt user for input
prompt_input() {
    local prompt_text="$1"
    local default_value="$2"
    local input

    if [ -n "$default_value" ]; then
        read -p "$prompt_text [$default_value]: " input
        echo "${input:-$default_value}"
    else
        read -p "$prompt_text: " input
        echo "$input"
    fi
}

# Function to deploy to API server (qisg)
deploy_api() {
    print_info "Starting deployment to API server (qisg)..."

    # SSH to qisg and deploy
    ssh qisg << 'ENDSSH'
        set -e
        # Add Go to PATH
        export PATH=$PATH:/usr/local/go/bin

        echo "[INFO] Deploying to API server..."

        # Navigate to project directory
        cd /root/nofx

        # Pull latest changes
        echo "[INFO] Pulling latest changes from git..."
        git pull

        # Build the project
        echo "[INFO] Building Go project..."
        go build -o nofx

        # Restart service
        echo "[INFO] Restarting nofx.service..."
        systemctl restart nofx.service

        # Check service status
        echo "[INFO] Checking service status..."
        systemctl status nofx.service --no-pager

        echo "[SUCCESS] API deployment completed!"
ENDSSH

    print_success "API deployment completed successfully!"
}

# Function to deploy to Web server (qibj)
deploy_web() {
    print_info "Starting deployment to Web server (qibj)..."

    # Get number of commits to deploy
    local num_commits=$(prompt_input "How many recent commits to deploy?" "1")

    # Get the list of changed files
    print_info "Getting changed files from last $num_commits commit(s)..."
    local changed_files=$(git diff --name-only HEAD~$num_commits HEAD | grep "^src/" || true)

    if [ -z "$changed_files" ]; then
        print_warning "No files changed in src/ directory. Checking for any changes..."
        changed_files=$(git diff --name-only HEAD~$num_commits HEAD || true)
    fi

    if [ -z "$changed_files" ]; then
        print_error "No files found to deploy. Aborting."
        exit 1
    fi

    print_info "Files to be deployed:"
    echo "$changed_files" | while read -r file; do
        echo "  - $file"
    done

    # Confirm deployment
    read -p "Continue with deployment? (y/n): " confirm
    if [[ ! $confirm =~ ^[Yy]$ ]]; then
        print_warning "Deployment cancelled by user."
        exit 0
    fi

    # Create temp directory for files
    local temp_dir=$(mktemp -d)
    print_info "Preparing files in temporary directory: $temp_dir"

    # Copy files to temp directory preserving structure
    echo "$changed_files" | while read -r file; do
        if [ -f "$file" ]; then
            mkdir -p "$temp_dir/$(dirname "$file")"
            cp "$file" "$temp_dir/$file"
            echo "Copied: $file"
        fi
    done

    # Also copy package.json and package-lock.json if they changed
    if git diff --name-only HEAD~$num_commits HEAD | grep -q "package.json"; then
        cp web/package.json "$temp_dir/"
        print_info "Including package.json"
    fi

    if git diff --name-only HEAD~$num_commits HEAD | grep -q "package-lock.json"; then
        cp web/package-lock.json "$temp_dir/"
        print_info "Including package-lock.json"
    fi

    # Upload files to qibj
    print_info "Uploading files to qibj..."
    scp -r "$temp_dir"/* root@qibj:/root/projects/nofx/web/

    # Clean up temp directory
    rm -rf "$temp_dir"

    # SSH to qibj to build and restart
    ssh qibj << 'ENDSSH'
        set -e
        echo "[INFO] Building and deploying on web server..."

        # Navigate to project directory
        cd /root/projects/nofx/web

        # Check if node_modules exists, if not run npm install
        if [ ! -d "node_modules" ]; then
            echo "[INFO] node_modules not found, running npm install..."
            npm install
        fi

        # Install dependencies if package.json changed
        if [ -f "package.json" ]; then
            echo "[INFO] Running npm install to ensure dependencies are up to date..."
            npm install
        fi

        # Build the project
        echo "[INFO] Building web project..."
        npm run build

        # Restart service
        echo "[INFO] Restarting nofx-web.service..."
        systemctl restart nofx-web.service

        # Check service status
        echo "[INFO] Checking service status..."
        systemctl status nofx-web.service --no-pager

        echo "[SUCCESS] Web deployment completed!"
ENDSSH

    print_success "Web deployment completed successfully!"
}

# Main deployment logic
main() {
    echo "=========================================="
    echo "      NOFX Deployment Script"
    echo "=========================================="
    echo ""
    echo "Select deployment target:"
    echo "  1) API Server (qisg)"
    echo "  2) Web Server (qibj)"
    echo "  3) Both"
    echo ""

    read -p "Enter choice [1-3]: " choice

    case $choice in
        1)
            deploy_api
            ;;
        2)
            deploy_web
            ;;
        3)
            print_info "Deploying to both servers..."
            deploy_api
            echo ""
            deploy_web
            ;;
        *)
            print_error "Invalid choice. Exiting."
            exit 1
            ;;
    esac

    echo ""
    print_success "All deployments completed! 🚀"
}

# Run main function
main
