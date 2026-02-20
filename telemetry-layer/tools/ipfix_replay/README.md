# IPFIX Replay Tool (E2E Testing)

This tool replays a **raw IPFIX UDP payload** to the gateway adapter. It supports raw, hex, and base64 payload files.

## Build
```
cd telemetry-layer/tools/ipfix_replay
go build
```

## Usage
```
./ipfix_replay --addr 127.0.0.1:2055 --input ipfix_payload.bin --format raw
./ipfix_replay --addr 127.0.0.1:2055 --input ipfix_payload.hex --format hex --count 5
```

## How to get a payload
Capture a real IPFIX packet from your gateway exporter and save it as:
- raw bytes (`.bin`)  
- hex string (`.hex`)  
- base64 string (`.b64`)  

Then replay it with this tool to validate the **gateway → adapter → Kafka → AI** path.
