# IPFIX Payload Generator

Generates a **strict IPFIX v10** payload (template + data set) for E2E testing.
Outputs raw bytes, hex, or base64 and can optionally send via UDP.

## Build
```
cd telemetry-layer/tools/ipfix_gen
go build
```

## Examples

Generate a raw payload file:
```
./ipfix_gen --output ipfix_payload.bin --format raw
```

Generate hex (for replay tool):
```
./ipfix_gen --output ipfix_payload.hex --format hex
```

Send directly to the gateway adapter:
```
./ipfix_gen --udp gateway-adapter-ipfix.telemetry-test.svc.cluster.local:2055
```

## Fields included
- sourceIPv4Address (8)
- destinationIPv4Address (12)
- sourceTransportPort (7)
- destinationTransportPort (11)
- protocolIdentifier (4)
- packetDeltaCount (2)
- octetDeltaCount (1)
- flowStartMilliseconds (152)
- flowEndMilliseconds (153)
