# Telemetry Image Builds

This folder defines minimal production images for telemetry components.

## Build Strategy

- Go components use one shared multi-stage Dockerfile: `docker/telemetry/go-component/Dockerfile`
- Feature transformer uses: `docker/telemetry/feature-transformer/Dockerfile`
- Build context is repo root (`B:\CyberMesh`)

## Images to Build

1. `telemetry-bridge`
2. `telemetry-stream-processor`
3. `telemetry-gateway-adapter`
4. `telemetry-baremetal-adapter`
5. `telemetry-cloudlogs-adapter`
6. `telemetry-feature-transformer`

## Build Commands

Set your registry first:

```powershell
$REGISTRY = "YOUR_REGISTRY"
```

Build Go images:

```powershell
docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=ingest-bridge_Go `
  --build-arg CMD_PATH=./cmd/bridge `
  --build-arg BIN_NAME=telemetry-bridge `
  -t "$REGISTRY/telemetry-bridge:latest" .

docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=stream-processor `
  --build-arg CMD_PATH=./cmd/processor `
  --build-arg BIN_NAME=telemetry-stream-processor `
  -t "$REGISTRY/telemetry-stream-processor:latest" .

docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=adapters `
  --build-arg CMD_PATH=./cmd/gateway `
  --build-arg BIN_NAME=telemetry-gateway-adapter `
  -t "$REGISTRY/telemetry-gateway-adapter:latest" .

docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=adapters `
  --build-arg CMD_PATH=./cmd/baremetal `
  --build-arg BIN_NAME=telemetry-baremetal-adapter `
  -t "$REGISTRY/telemetry-baremetal-adapter:latest" .

docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=adapters `
  --build-arg CMD_PATH=./cmd/cloudlogs `
  --build-arg BIN_NAME=telemetry-cloudlogs-adapter `
  -t "$REGISTRY/telemetry-cloudlogs-adapter:latest" .
```

Build Python transformer image:

```powershell
docker build -f docker/telemetry/feature-transformer/Dockerfile `
  -t "$REGISTRY/telemetry-feature-transformer:latest" .
```

## Optional: PCAP Service

If you want a dedicated PCAP service image:

```powershell
docker build -f docker/telemetry/go-component/Dockerfile `
  --build-arg MODULE_DIR=pcap-service `
  --build-arg CMD_PATH=./cmd/pcap `
  --build-arg BIN_NAME=telemetry-pcap-service `
  -t "$REGISTRY/telemetry-pcap-service:latest" .
```
