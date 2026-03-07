#!/bin/bash

# 
# AegisTrade AI Trading System - Docker Quick Start Script
# Usage: ./start.sh [command]
# 

set -e

# ------------------------------------------------------------------------
# Color Definitions
# ------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ------------------------------------------------------------------------
# Utility Functions: Colored Output
# ------------------------------------------------------------------------
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

# ------------------------------------------------------------------------
# Detection: Docker Compose Command (Backward Compatible)
# ------------------------------------------------------------------------
detect_compose_cmd() {
    if command -v docker compose &> /dev/null; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose &> /dev/null; then
        COMPOSE_CMD="docker-compose"
    else
        print_error "Docker Compose  Docker Compose"
        exit 1
    fi
    print_info " Docker Compose : $COMPOSE_CMD"
}

# ------------------------------------------------------------------------
# Validation: Docker Installation
# ------------------------------------------------------------------------
check_docker() {
    if ! command -v docker &> /dev/null; then
        print_error "Docker  Docker: https://docs.docker.com/get-docker/"
        exit 1
    fi

    detect_compose_cmd
    print_success "Docker  Docker Compose "
}

# ------------------------------------------------------------------------
# Validation: Environment File (.env)
# ------------------------------------------------------------------------
check_env() {
    if [ ! -f ".env" ]; then
        print_warning ".env ..."
        cp .env.example .env
        print_info " .env "
        print_info ": nano .env "
        exit 1
    fi
    print_success ""
}

# ------------------------------------------------------------------------
# Validation: Configuration File (config.json)
# ------------------------------------------------------------------------
check_config() {
    if [ ! -f "config.json" ]; then
        print_warning "config.json ..."
        cp config.json.example config.json
        print_info " config.json  API "
        print_info ": nano config.json "
        exit 1
    fi
    print_success ""
}

# ------------------------------------------------------------------------
# Build: Frontend (Node.js Based)
# ------------------------------------------------------------------------
# build_frontend() {
#     print_info "..."

#     if ! command -v node &> /dev/null; then
#         print_error "Node.js  Node.js"
#         exit 1
#     fi

#     if ! command -v npm &> /dev/null; then
#         print_error "npm  npm"
#         exit 1
#     fi

#     print_info "..."
#     cd web

#     print_info " Node.js ..."
#     npm install

#     print_info "..."
#     npm run build

#     cd ..
#     print_success ""
# }

# ------------------------------------------------------------------------
# Service Management: Start
# ------------------------------------------------------------------------
start() {
    print_info " AegisTrade AI Trading System..."

    # Auto-build frontend if missing or forced
    # if [ ! -d "web/dist" ] || [ "$1" == "--build" ]; then
    #     build_frontend
    # fi

    # Rebuild images if flag set
    if [ "$1" == "--build" ]; then
        print_info "..."
        $COMPOSE_CMD up -d --build
    else
        print_info "..."
        $COMPOSE_CMD up -d
    fi

    print_success ""
    print_info "Web : http://localhost:3000"
    print_info "API : http://localhost:8080"
    print_info ""
    print_info ": ./start.sh logs"
    print_info ": ./start.sh stop"
}

# ------------------------------------------------------------------------
# Service Management: Stop
# ------------------------------------------------------------------------
stop() {
    print_info "..."
    $COMPOSE_CMD stop
    print_success ""
}

# ------------------------------------------------------------------------
# Service Management: Restart
# ------------------------------------------------------------------------
restart() {
    print_info "..."
    $COMPOSE_CMD restart
    print_success ""
}

# ------------------------------------------------------------------------
# Monitoring: Logs
# ------------------------------------------------------------------------
logs() {
    if [ -z "$2" ]; then
        $COMPOSE_CMD logs -f
    else
        $COMPOSE_CMD logs -f "$2"
    fi
}

# ------------------------------------------------------------------------
# Monitoring: Status
# ------------------------------------------------------------------------
status() {
    print_info ":"
    $COMPOSE_CMD ps
    echo ""
    print_info ":"
    curl -s http://localhost:8080/health | jq '.' || echo ""
}

# ------------------------------------------------------------------------
# Maintenance: Clean (Destructive)
# ------------------------------------------------------------------------
clean() {
    print_warning ""
    read -p "(yes/no): " confirm
    if [ "$confirm" == "yes" ]; then
        print_info "..."
        $COMPOSE_CMD down -v
        print_success ""
    else
        print_info ""
    fi
}

# ------------------------------------------------------------------------
# Maintenance: Update
# ------------------------------------------------------------------------
update() {
    print_info "..."
    git pull
    $COMPOSE_CMD up -d --build
    print_success ""
}

# ------------------------------------------------------------------------
# Help: Usage Information
# ------------------------------------------------------------------------
show_help() {
    echo "AegisTrade AI Trading System - Docker "
    echo ""
    echo ": ./start.sh [command] [options]"
    echo ""
    echo ":"
    echo "  start [--build]    "
    echo "  stop               "
    echo "  restart            "
    echo "  logs [service]      backend/frontend"
    echo "  status             "
    echo "  clean              "
    echo "  update             "
    echo "  help               "
    echo ""
    echo ":"
    echo "  ./start.sh start --build    # "
    echo "  ./start.sh logs backend     # "
    echo "  ./start.sh status           # "
}

# ------------------------------------------------------------------------
# Main: Command Dispatcher
# ------------------------------------------------------------------------
main() {
    check_docker

    case "${1:-start}" in
        start)
            check_env
            check_config
            start "$2"
            ;;
        stop)
            stop
            ;;
        restart)
            restart
            ;;
        logs)
            logs "$@"
            ;;
        status)
            status
            ;;
        clean)
            clean
            ;;
        update)
            update
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            print_error ": $1"
            show_help
            exit 1
            ;;
    esac
}

# Execute Main
main "$@"