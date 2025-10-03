#!/bin/bash

# Default configuration
DATADIR="json-bench-data"
SNAPSHOT_PATH=""
SNAPSHOT_URL=""
CLIENT=""
NETWORK="mainnet"
CUSTOM_IMAGE=""
EXTRA_ARGS=""
RPC_PORT="8545"

# Function to display usage
usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -n, --network NETWORK   Ethereum network (default: mainnet)"
    echo "  -d, --datadir PATH      Data directory for overlay filesystem (default: json-bench-data)"
    echo "  -s, --snapshot PATH     Path to the snapshot directory (required)"
    echo "  -u, --snapshot-url URL  URL to download snapshot from (optional)"
    echo "  -c, --client CLIENT     Client type for Docker command (required)"
    echo "  -i, --image IMAGE       Custom Docker image (overrides client default)"
    echo "  -a, --args ARGS         Extra arguments to pass to the client (in addition to defaults)"
    echo "  -p, --port PORT         RPC port for the client (default: 8545)"
    echo "  -h, --help              Show this help message"
    echo ""
    echo "Examples:"
    echo "  # Use existing snapshot directory"
    echo "  $0 -s /path/to/snapshot -c nethermind"
    echo ""
    echo "  # Download snapshot from ethPandaOps (direct file URL)"
    echo "  $0 -n hoodi -s ./snapshots -u https://snapshots.ethpandaops.io/hoodi/geth/12345678/snapshot.tar.zst -c geth"
    echo ""
    echo "  # Download with custom datadir and network"
    echo "  $0 -n mainnet -s ./snapshots -u https://snapshots.ethpandaops.io/mainnet/nethermind/12345678/snapshot.tar.zst -c nethermind -d /custom/datadir"
    echo ""
    echo "  # Use custom Docker image and extra arguments"
    echo "  $0 -s ./snapshots -c nethermind -i nethermind/nethermind:v1.20.0 -a '--JsonRpc.AdditionalRpcUrls=http://0.0.0.0:8546'"
    echo ""
    echo "  # Use custom RPC port"
    echo "  $0 -s ./snapshots -c geth -p 8546"
}

# Function to download and extract snapshot
download_snapshot() {
    echo "Downloading snapshot from: $SNAPSHOT_URL"
    
    # Check if snapshot directory already exists
    if [[ -d "$SNAPSHOT_PATH" ]]; then
        echo "Error: Snapshot directory '$SNAPSHOT_PATH' already exists"
        echo "Please remove the existing directory, choose a different path or remove the --snapshot-url flag"
        exit 1
    fi
    
    # Create snapshot directory
    mkdir -p "$SNAPSHOT_PATH"
    
    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        echo "Error: Docker is required but not installed"
        exit 1
    fi
    
    echo "Using Docker for download and extraction..."
    # Use Docker approach similar to ethPandaOps quickstart
    docker run --rm -it \
        -v "$(pwd)/$SNAPSHOT_PATH:/data" \
        --entrypoint "/bin/sh" \
        alpine -c \
        'apk add --no-cache wget curl tar zstd && \
        echo "Downloading snapshot..." && \
        wget --tries=0 --retry-connrefused -O - "'"$SNAPSHOT_URL"'" | \
        tar -I zstd -xvf - -C /data'
    
    if [[ $? -ne 0 ]]; then
        echo "Error: Failed to download and extract snapshot using Docker"
        exit 1
    fi
    
    echo "Snapshot downloaded and extracted successfully to: $SNAPSHOT_PATH"
    
    # Verify the snapshot directory has content
    if [[ -z "$(ls -A "$SNAPSHOT_PATH" 2>/dev/null)" ]]; then
        echo "Error: Snapshot directory is empty after extraction"
        exit 1
    fi
    
    echo "Snapshot verification: $(ls -la "$SNAPSHOT_PATH" | wc -l) items found"
}

# Function to setup overlay filesystem
setup_overlay() {
    echo "Setting up overlay filesystem..."
    
    # Validate required parameters
    if [[ -z "$SNAPSHOT_PATH" ]]; then
        echo "Error: --snapshot is required"
        usage
        exit 1
    fi
    
    # Convert paths to absolute using realpath
    SNAPSHOT_PATH=$(realpath "$SNAPSHOT_PATH")
    DATADIR=$(realpath "$DATADIR")
    
    echo "Using absolute paths:"
    echo "  Snapshot: $SNAPSHOT_PATH"
    echo "  Data Dir: $DATADIR"
    
    # Download snapshot if URL is provided
    if [[ -n "$SNAPSHOT_URL" ]]; then
        download_snapshot
    fi
    
    if [[ ! -d "$SNAPSHOT_PATH" ]]; then
        echo "Error: Snapshot path '$SNAPSHOT_PATH' does not exist or is not a directory"
        exit 1
    fi
    
    # Set up overlay directories under datadir
    WORK_DIR="$DATADIR/work"
    UPPER_DIR="$DATADIR/upper"
    MERGED_DIR="$DATADIR/merged"
    
    # Create datadir and overlay subdirectories
    mkdir -p "$DATADIR"
    mkdir -p "$WORK_DIR"
    mkdir -p "$UPPER_DIR"
    mkdir -p "$MERGED_DIR"
    
    echo "Created directories:"
    echo "  Data Dir: $DATADIR"
    echo "  Work: $WORK_DIR"
    echo "  Upper: $UPPER_DIR"
    echo "  Merged: $MERGED_DIR"
    echo "  Snapshot: $SNAPSHOT_PATH"
    
    # Mount overlay filesystem
    echo "Mounting overlay filesystem..."
    if sudo mount -t overlay overlay -o lowerdir="$SNAPSHOT_PATH",upperdir="$UPPER_DIR",workdir="$WORK_DIR" "$MERGED_DIR"; then
        echo "Overlay filesystem mounted successfully at: $MERGED_DIR"
    else
        echo "Error: Failed to mount overlay filesystem"
        exit 1
    fi
}

# Function to cleanup overlay filesystem
cleanup_overlay() {
    echo ""
    echo "Cleaning up overlay filesystem..."
    
    # Unmount overlay if mounted
    if mountpoint -q "$MERGED_DIR"; then
        sudo umount "$MERGED_DIR"
        echo "Unmounted overlay filesystem from: $MERGED_DIR"
    fi
    
    # Remove directories
    rm -rf "$WORK_DIR" "$UPPER_DIR" "$MERGED_DIR"
    echo "Removed overlay directories"
}

# Function to run Docker command based on client
run_docker_command() {
    if [[ -z "$CLIENT" ]]; then
        echo "Error: --client is required"
        usage
        exit 1
    fi
    
    echo "Running Docker command for client: $CLIENT on network: $NETWORK"
    echo "Overlay mounted at: $MERGED_DIR"
    
    EL_IMAGE=""
    EL_ARGS=""
    # Client-specific Docker commands with network configuration
    case "$CLIENT" in
        "nethermind")
            EL_IMAGE="nethermind/nethermind:latest"
            case "$NETWORK" in
                "mainnet")
                    EL_ARGS="--config=mainnet"
                    EL_ARGS="$EL_ARGS --Init.BaseDbPath=mainnet"
                    ;;
                *)
                    echo "Error: Network '$NETWORK' is not supported for Nethermind client"
                    echo "Supported networks: mainnet"
                    exit 1
                    ;;
            esac
            EL_ARGS="$EL_ARGS --datadir=/execution-data"
            EL_ARGS="$EL_ARGS --JsonRpc.Enabled=true"
            EL_ARGS="$EL_ARGS --JsonRpc.Host=0.0.0.0"
            EL_ARGS="$EL_ARGS --JsonRpc.Port=$RPC_PORT"
            EL_ARGS="$EL_ARGS --Init.WebSocketsEnabled=true"
            EL_ARGS="$EL_ARGS --JsonRpc.WebSocketsPort=$RPC_PORT"
            EL_ARGS="$EL_ARGS --JsonRpc.EnabledModules=Eth,Subscribe,Trace,TxPool,Web3,Personal,Proof,Net,Parity,Health,Rpc,Debug,Admin"
            ;;
        "geth")
            EL_IMAGE="ethereum/client-go:latest"
            case "$NETWORK" in
                "mainnet")
                    EL_ARGS="--mainnet"
                    EL_ARGS="$EL_ARGS --syncmode=full"
                    ;;
                *)
                    echo "Error: Network '$NETWORK' is not supported for Geth client"
                    echo "Supported networks: mainnet"
                    exit 1
                    ;;
            esac
            EL_ARGS="$EL_ARGS --datadir=/execution-data"
            EL_ARGS="$EL_ARGS --http"
            EL_ARGS="$EL_ARGS --http.addr=0.0.0.0"
            EL_ARGS="$EL_ARGS --http.port=$RPC_PORT"
            EL_ARGS="$EL_ARGS --http.vhosts=*"
            EL_ARGS="$EL_ARGS --http.api=eth,net,web3,debug,admin"
            EL_ARGS="$EL_ARGS --ws"
            EL_ARGS="$EL_ARGS --ws.addr=0.0.0.0"
            EL_ARGS="$EL_ARGS --ws.port=$RPC_PORT"
            EL_ARGS="$EL_ARGS --ws.api=eth,web3,net,debug,admin"
            ;;
        # "erigon")
        #     case "$NETWORK" in
        #         "mainnet")
        #             echo "Executing Erigon Docker command for mainnet..."
        #             echo "Docker command is running. Press Ctrl+C to stop..."
        #             # Add your Erigon mainnet-specific Docker command here
        #             # Example: docker run --rm -it -v "$MERGED_DIR:/data" thorax/erigon:latest --chain mainnet
        #             ;;
        #         *)
        #             echo "Error: Network '$NETWORK' is not supported for Erigon client"
        #             echo "Supported networks: mainnet"
        #             exit 1
        #             ;;
        #     esac
        #     ;;
        # "besu")
        #     case "$NETWORK" in
        #         "mainnet")
        #             echo "Executing Besu Docker command for mainnet..."
        #             echo "Docker command is running. Press Ctrl+C to stop..."
        #             # Add your Besu mainnet-specific Docker command here
        #             # Example: docker run --rm -it -v "$MERGED_DIR:/data" besu/besu:latest --network=mainnet
        #             ;;
        #         *)
        #             echo "Error: Network '$NETWORK' is not supported for Besu client"
        #             echo "Supported networks: mainnet"
        #             exit 1
        #             ;;
        #     esac
        #     ;;
        # "reth")
        #     case "$NETWORK" in
        #         "mainnet")
        #             echo "Executing Reth Docker command for mainnet..."
        #             echo "Docker command is running. Press Ctrl+C to stop..."
        #             # Add your Reth mainnet-specific Docker command here
        #             # Example: docker run --rm -it -v "$MERGED_DIR:/data" reth/reth:latest --chain mainnet
        #             ;;
        #         *)
        #             echo "Error: Network '$NETWORK' is not supported for Reth client"
        #             echo "Supported networks: mainnet"
        #             exit 1
        #             ;;
        #     esac
        #     ;;
        *)
            echo "Unknown client: $CLIENT"
            echo "Available clients: nethermind, geth"
            exit 1
            ;;
    esac

    # Validate that client configuration was set
    if [[ -z "$EL_IMAGE" ]]; then
        echo "Error: No Docker image configured for client '$CLIENT'"
        exit 1
    fi
    
    if [[ -z "$EL_ARGS" ]]; then
        echo "Error: No arguments configured for client '$CLIENT'"
        exit 1
    fi
    
    # Use custom image if provided, otherwise use client default
    FINAL_IMAGE="${CUSTOM_IMAGE:-$EL_IMAGE}"
    
    # Combine default args with extra args
    FINAL_ARGS="${EL_ARGS}"
    if [[ -n "$EXTRA_ARGS" ]]; then
        FINAL_ARGS="${FINAL_ARGS} ${EXTRA_ARGS}"
    fi
    
    echo "Running Docker command for client: $CLIENT on network: $NETWORK"
    echo "Docker image: $FINAL_IMAGE"
    echo "Docker command is running. Press Ctrl+C to stop..."
    
    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        echo "Error: Docker is required but not installed"
        exit 1
    fi
    
    # Execute Docker command
    docker run --rm -it \
        -v "$MERGED_DIR:/execution-data" \
        -p $RPC_PORT:$RPC_PORT \
        ${FINAL_IMAGE} \
        ${FINAL_ARGS}
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -d|--datadir)
            DATADIR="$2"
            shift 2
            ;;
        -s|--snapshot)
            SNAPSHOT_PATH="$2"
            shift 2
            ;;
        -u|--snapshot-url)
            SNAPSHOT_URL="$2"
            shift 2
            ;;
        -c|--client)
            CLIENT="$2"
            shift 2
            ;;
        -n|--network)
            NETWORK="$2"
            shift 2
            ;;
        -i|--image)
            CUSTOM_IMAGE="$2"
            shift 2
            ;;
        -a|--args)
            EXTRA_ARGS="$2"
            shift 2
            ;;
        -p|--port)
            RPC_PORT="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Set up signal handlers for cleanup
trap cleanup_overlay EXIT INT TERM

# Main execution
echo "Starting overlay setup and Docker execution..."
echo "Configuration:"
echo "  Data Dir: $DATADIR"
echo "  Snapshot: $SNAPSHOT_PATH"
if [[ -n "$SNAPSHOT_URL" ]]; then
    echo "  Snapshot URL: $SNAPSHOT_URL"
fi
echo "  Client: $CLIENT"
echo "  Network: $NETWORK"
echo "  RPC Port: $RPC_PORT"
if [[ -n "$CUSTOM_IMAGE" ]]; then
    echo "  Custom Image: $CUSTOM_IMAGE"
fi
if [[ -n "$EXTRA_ARGS" ]]; then
    echo "  Extra Args: $EXTRA_ARGS"
fi
echo ""

# Step 1: Setup overlay filesystem
setup_overlay

# Step 2: Run Docker command
run_docker_command
