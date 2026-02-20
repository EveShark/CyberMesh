# Adapter Input Contracts (Step 7)

This directory documents **production‑grade** adapter inputs and how they map to `flow.v1`. Adapters accept JSON lines and can parse either:

1) **`normalized`** input (already in `flow.v1` field names)  
2) **`mapped_json`** input (source‑specific fields mapped via a mapping file)

## 1) Formats

### `normalized` (default)
Use JSON lines with fields that match `flow.v1`. This is best for internal or pre‑normalized sources.

Example line:
```
{"ts":1700000000,"tenant_id":"t1","src_ip":"10.0.0.1","dst_ip":"10.0.0.2","src_port":443,"dst_port":8080,"proto":6,"bytes_fwd":1200,"pkts_fwd":3}
```

### `mapped_json`
Use this for cloud logs, gateway export, or any raw source. Provide a mapping file to translate source fields into `flow.v1`.

Env:
```
RECORD_FORMAT=mapped_json
RECORD_MAPPING_PATH=./mappings/normalized.json
```

### `gcp_vpc_raw`
Use this for raw GCP VPC Flow Log entries (JSON). The adapter reads a JSON line with `jsonPayload.*` and `resource.labels.*`.

Env:
```
RECORD_FORMAT=gcp_vpc_raw
```

### `aws_vpc_raw`
Use this for AWS VPC flow logs in the default space‑delimited format (version 2).

Env:
```
RECORD_FORMAT=aws_vpc_raw
```

### `azure_nsg_raw`
Use this for Azure NSG flow logs containing `flowTuples`.

Env:
```
RECORD_FORMAT=azure_nsg_raw
```

### `ipfix_udp`
Use this for IPFIX/NetFlow v10 exporters over UDP (hardware gateways, routers).

Env:
```
RECORD_FORMAT=ipfix_udp
IPFIX_LISTEN_ADDR=:2055
TENANT_ID=your-tenant
```

## 2) Mapping Spec
Mapping file is JSON:

```
{
  "version": "v1",
  "fields": {
    "tenant_id": { "path": "resource.project", "type": "string", "required": true },
    "src_ip":    { "path": "connection.src_ip", "type": "string", "required": true }
  }
}
```

**Field keys** must be one of:
`ts`, `tenant_id`, `src_ip`, `dst_ip`, `src_port`, `dst_port`, `proto`, `bytes_fwd`, `bytes_bwd`,
`pkts_fwd`, `pkts_bwd`, `duration_ms`, `verdict`, `metrics_known`, `source_type`, `source_id`,
`timing_known`, `timing_derived`, `derivation_policy`, `flags_known`, all IAT/active/idle/flag fields,
and identity fields: `identity.namespace`, `identity.pod`, `identity.node`.

**Supported types**:
- `string`, `int`, `int64`, `float`, `bool`
- `timestamp` (RFC3339/RFC3339Nano → unix seconds)
- `proto` (maps `TCP/UDP/ICMP` to `6/17/1`)

**Const values**:
```
{"metrics_known": {"const":"true","type":"bool"}}
```

## 3) Source‑Specific Inputs (what to wire)

### Hubble / Cilium
- Use `normalized` if your gRPC exporter already emits `flow.v1`.
- Otherwise use `mapped_json` with a mapping file aligned to your exported JSON schema.

### Cloud flow logs (GCP / AWS / Azure)
- Use `mapped_json` and a mapping file per provider export schema.
- Required: `tenant_id`, `src_ip`, `dst_ip`, `src_port`, `dst_port`, `proto`, `ts`.
- Optional: identity, verdict, bytes/packets, duration, flags, timing.
  - GCP: `mappings/gcp_vpc.json`
  - AWS: `mappings/aws_vpc.json`
  - Azure: `mappings/azure_nsg.json`
  - Raw formats: `gcp_vpc_raw`, `aws_vpc_raw`, `azure_nsg_raw`

### Gateway export / inline sensor
- Use `mapped_json` to map gateway fields into `flow.v1`.
- If you already produce normalized JSON, use `normalized`.
  - Gateway: `mappings/gateway.json`

## 4) Security & Validation
- Missing required fields → DLQ.
- Schema registry headers are attached for Protobuf payloads.
- `metrics_known` auto‑defaults to `true` if bytes/packets were provided and not explicitly set.

## 5) Example Mapping
`telemetry-layer/adapters/mappings/normalized.json` is a full example for normalized inputs.
