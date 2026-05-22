#!/usr/bin/env bash

set -e

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

if [ ! -d "$SCRIPT_DIR/../data" ]; then
  mkdir -p "$SCRIPT_DIR/../data"
fi

data_dir=$(readlink -f "$SCRIPT_DIR/../data")

bold=$(tput bold)
normal=$(tput sgr0)

mkdir -p "$data_dir/postgres"

# Detect container runtime (podman/docker)
if command -v container &>/dev/null; then
    RUNTIME="container"
elif command -v podman &>/dev/null; then
    RUNTIME="podman"
elif command -v docker &>/dev/null; then
    RUNTIME="docker"
else
    echo "Error: No container runtime found (container/podman/docker)" >&2
    exit 1
fi

# Ensure container system is running (macOS podman machine)
if [ "$RUNTIME" = "container" ]; then
    if $RUNTIME system status 2>&1 | grep -q "is not running"; then
        echo "Container system is not running. Starting it..."
        $RUNTIME system start
    fi
fi

# Parse flags
DETACHED=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        -d) DETACHED=true ;;
        *) echo "Usage: $0 [-d]" >&2; exit 1 ;;
    esac
    shift
done

# Cleanup function to stop containers
cleanup() {
    if [ "$DETACHED" = false ]; then
        echo ""
        echo "Stopping containers..."
        $RUNTIME stop postgres-waitinglist 2>/dev/null || true
        $RUNTIME rm postgres-waitinglist 2>/dev/null || true
    fi
    exit 0
}

# Set up signal handlers for graceful shutdown
trap cleanup SIGINT SIGTERM

# Check if container exists and its state
container_exists() {
    $RUNTIME inspect "$1" >/dev/null 2>&1
    return $?
}

container_is_running() {
    [ "$($RUNTIME inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]
}

function _printPostgresURI() {
    echo ""
    echo "${bold}PostgreSQL is ready:${normal}"
    echo "  URI: postgres://brain:brain@localhost:5432/waitinglist?sslmode=disable"
    echo ""
    echo "  To run the service:"
    echo "    make run"
    echo ""
}

CONTAINER_NAME="postgres-waitinglist"

if [ "$DETACHED" = true ]; then
    echo "Starting PostgreSQL container (detached)..."
    if container_exists $CONTAINER_NAME; then
        if container_is_running $CONTAINER_NAME; then
            echo "$CONTAINER_NAME container already running"
        else
            echo "Starting existing $CONTAINER_NAME container..."
            $RUNTIME start $CONTAINER_NAME
        fi
    else
        $RUNTIME run -d \
            --name $CONTAINER_NAME \
            -v "$data_dir/postgres:/var/lib/postgresql/data" \
            -p 5432:5432 \
            -e POSTGRES_DB=waitinglist \
            -e POSTGRES_USER=brain \
            -e POSTGRES_PASSWORD=brain \
            postgres:17
    fi

    _printPostgresURI
    echo "Container started in detached mode. Stop with: $RUNTIME stop $CONTAINER_NAME"
else
    echo "Starting PostgreSQL in foreground mode (Ctrl+C to stop)..."

    # Remove any stale container from previous runs
    $RUNTIME rm -f $CONTAINER_NAME 2>/dev/null || true

    _printPostgresURI

    $RUNTIME run --rm \
        --name $CONTAINER_NAME \
        -v "$data_dir/postgres:/var/lib/postgresql/data" \
        -p 5432:5432 \
        -e POSTGRES_DB=waitinglist \
        -e POSTGRES_USER=brain \
        -e POSTGRES_PASSWORD=brain \
        postgres:17 2>&1 | sed "s/^/[postgres] /"
fi
