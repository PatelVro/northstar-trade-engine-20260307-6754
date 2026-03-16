#!/bin/bash

# Northstar Trading Bot - PM2 
# : ./pm2.sh [start|stop|restart|status|logs|build]

set -e

# 
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

# 
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 
print_info() {
    echo -e "${BLUE}  $1${NC}"
}

print_success() {
    echo -e "${GREEN} $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}  $1${NC}"
}

print_error() {
    echo -e "${RED} $1${NC}"
}

print_header() {
    echo -e "${PURPLE}${NC}"
    echo -e "${PURPLE}   Northstar Trading Bot - PM2 Manager${NC}"
    echo -e "${PURPLE}${NC}"
    echo ""
}

#  PM2 
check_pm2() {
    if ! command -v pm2 &> /dev/null; then
        print_error "PM2 : npm install -g pm2"
        exit 1
    fi
}

# 
ensure_log_dirs() {
    mkdir -p "$PROJECT_ROOT/logs"
    mkdir -p "$PROJECT_ROOT/web/logs"
    print_info ""
}

# 
build_backend() {
    local version="${NORTHSTAR_VERSION:-dev}"
    local commit="${NORTHSTAR_COMMIT:-unknown}"
    local build_time="${NORTHSTAR_BUILD_TIME:-unknown}"
    local channel="${NORTHSTAR_BUILD_CHANNEL:-pm2}"
    local dirty="${NORTHSTAR_BUILD_DIRTY:-unknown}"

    if command -v git &> /dev/null && git rev-parse --is-inside-work-tree &> /dev/null; then
        if [ "$version" = "dev" ]; then
            version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
        fi
        if [ "$commit" = "unknown" ]; then
            commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
        fi
        if [ "$build_time" = "unknown" ]; then
            build_time="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
        fi
        if [ "$dirty" = "unknown" ]; then
            if [ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]; then
                dirty="dirty"
            else
                dirty="clean"
            fi
        fi
    fi

    local ldflags="-s -w -X northstar/buildinfo.Version=${version} -X northstar/buildinfo.Commit=${commit} -X northstar/buildinfo.BuildTime=${build_time} -X northstar/buildinfo.Channel=${channel} -X northstar/buildinfo.Dirty=${dirty}"

    print_info "..."
    print_info " build=${version} commit=${commit} dirty=${dirty}"
    go build -trimpath -ldflags "$ldflags" -o northstar
    if [ $? -eq 0 ]; then
        print_success ""
    else
        print_error ""
        exit 1
    fi
}

# 
build_frontend() {
    print_info "..."
    cd web
    npm run build
    if [ $? -eq 0 ]; then
        print_success ""
        cd ..
    else
        print_error ""
        exit 1
    fi
}

# 
start_services() {
    print_header
    ensure_log_dirs

    # 
    if [ ! -f "./northstar" ]; then
        print_warning "..."
        build_backend
    fi

    print_info "..."
    pm2 start pm2.config.js

    sleep 2
    pm2 status

    echo ""
    print_success ""
    echo ""
    echo -e "${CYAN} :${NC}"
    echo -e "  ${GREEN}:${NC} http://localhost:3000"
    echo -e "  ${GREEN} API:${NC} http://localhost:8080"
    echo ""
    echo -e "${CYAN} :${NC}"
    echo -e "  ${GREEN}:${NC} ./pm2.sh logs"
    echo -e "  ${GREEN}:${NC} ./pm2.sh logs backend"
    echo -e "  ${GREEN}:${NC} ./pm2.sh logs frontend"
    echo ""
}

# 
stop_services() {
    print_header
    print_info "..."
    pm2 stop pm2.config.js
    print_success ""
}

# 
restart_services() {
    print_header
    print_info "..."
    pm2 restart pm2.config.js
    sleep 2
    pm2 status
    print_success ""
}

# 
delete_services() {
    print_header
    print_warning " PM2 ..."
    pm2 delete pm2.config.js || true
    print_success "PM2 "
}

# 
show_status() {
    print_header
    pm2 status
    echo ""
    print_info ":"
    pm2 info northstar-backend
    echo ""
    pm2 info northstar-frontend
}

# 
show_logs() {
    if [ -z "$2" ]; then
        # 
        pm2 logs
    elif [ "$2" = "backend" ]; then
        pm2 logs northstar-backend
    elif [ "$2" = "frontend" ]; then
        pm2 logs northstar-frontend
    else
        print_error ": $2"
        print_info ": ./pm2.sh logs [backend|frontend]"
        exit 1
    fi
}

# 
show_monitor() {
    print_header
    print_info " PM2 ..."
    pm2 monit
}

# 
rebuild_and_restart() {
    print_header
    print_info "..."
    build_backend

    print_info "..."
    pm2 restart northstar-backend

    sleep 2
    pm2 status
    print_success ""
}

# 
show_help() {
    print_header
    echo -e "${CYAN}:${NC}"
    echo "  ./pm2.sh [command]"
    echo ""
    echo -e "${CYAN}:${NC}"
    echo -e "  ${GREEN}start${NC}       - "
    echo -e "  ${GREEN}stop${NC}        - "
    echo -e "  ${GREEN}restart${NC}     - "
    echo -e "  ${GREEN}status${NC}      - "
    echo -e "  ${GREEN}logs${NC}        -  (Ctrl+C )"
    echo -e "  ${GREEN}logs backend${NC}  - "
    echo -e "  ${GREEN}logs frontend${NC} - "
    echo -e "  ${GREEN}monitor${NC}     -  PM2 "
    echo -e "  ${GREEN}build${NC}       - "
    echo -e "  ${GREEN}rebuild${NC}     - "
    echo -e "  ${GREEN}delete${NC}      -  PM2 "
    echo -e "  ${GREEN}help${NC}        - "
    echo ""
    echo -e "${CYAN}:${NC}"
    echo "  ./pm2.sh start          # "
    echo "  ./pm2.sh logs backend   # "
    echo "  ./pm2.sh rebuild        # "
    echo ""
}

# 
check_pm2

case "${1:-help}" in
    start)
        start_services
        ;;
    stop)
        stop_services
        ;;
    restart)
        restart_services
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs "$@"
        ;;
    monitor|mon)
        show_monitor
        ;;
    build)
        build_backend
        ;;
    rebuild)
        rebuild_and_restart
        ;;
    delete|remove)
        delete_services
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        print_error ": $1"
        echo ""
        show_help
        exit 1
        ;;
esac
