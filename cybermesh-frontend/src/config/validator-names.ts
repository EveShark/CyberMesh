/**
 * Validator Node Name Mapping
 * 
 * Maps real blockchain node IDs (hex addresses) to friendly constellation names.
 * These names are displayed in the UI for better readability.
 * 
 * To add a new node, add its full hex ID as the key and the friendly name as the value.
 * The lookup supports both full and partial (prefix) matching.
 */

export interface ValidatorNameConfig {
    id: string;
    name: string;
    shortId: string; // First 10 chars of the ID
}

// Full node ID to friendly name mapping
// Update these when new validators are added or IDs change
// Based on actual backend IDs observed in the system
const NODE_NAME_MAP: Record<string, string> = {
    // Node ID prefixes (first 10-12 hex chars after 0x)
    "0x25a3b9156c": "Orion",
    "0x2fce8f4b85": "Lyra",
    "0x6265114dda": "Draco",
    "0x8bf26c134a": "Cygnus",
    "0xebe9e93909": "Vela",
    "0xd9661d47": "Antares",

    // Alternative prefix patterns (longer matches)
    "0x091b": "Orion",     // seen as 0x091b…ee11
    "0xeb4f": "Lyra",      // seen as 0xeb4f…9235
    "0x5196": "Draco",     // seen as 0x5196…7bfa
    "0x8cd3": "Cygnus",    // seen as 0x8cd3…a711
    "0x03be": "Vela",      // seen as 0x03be…cdfd
};

// Suffix patterns for additional matching
const NODE_SUFFIX_MAP: Record<string, string> = {
    "ee11": "Orion",
    "9235": "Lyra",
    "7bfa": "Draco",
    "a711": "Cygnus",
    "cdfd": "Vela",
};

// Reverse lookup: name to ID prefix
const NAME_TO_ID_PREFIX: Record<string, string> = {
    "Orion": "0x25a3b9156c",
    "Lyra": "0x2fce8f4b85",
    "Draco": "0x6265114dda",
    "Cygnus": "0x8bf26c134a",
    "Vela": "0xebe9e93909",
};

/**
 * Get friendly name for a node ID
 * Supports full ID match, prefix match, suffix match, and partial match
 */
export function getNodeName(nodeId: string | undefined | null): string {
    if (!nodeId) return "Unknown";

    const normalizedId = nodeId.toLowerCase();

    // Try exact match first
    if (NODE_NAME_MAP[normalizedId]) {
        return NODE_NAME_MAP[normalizedId];
    }

    // Try prefix match (first 12 chars: "0x" + 10 hex chars)
    const prefix12 = normalizedId.slice(0, 12);
    if (NODE_NAME_MAP[prefix12]) {
        return NODE_NAME_MAP[prefix12];
    }

    // Try shorter prefix match (first 6 chars: "0x" + 4 hex chars)
    const prefix6 = normalizedId.slice(0, 6);
    if (NODE_NAME_MAP[prefix6]) {
        return NODE_NAME_MAP[prefix6];
    }

    // Try suffix match (last 4 chars)
    const suffix4 = normalizedId.slice(-4);
    if (NODE_SUFFIX_MAP[suffix4]) {
        return NODE_SUFFIX_MAP[suffix4];
    }

    // Try to find any key that starts with or is a prefix of the nodeId
    for (const [key, name] of Object.entries(NODE_NAME_MAP)) {
        if (normalizedId.startsWith(key) || key.startsWith(normalizedId)) {
            return name;
        }
    }

    // Fallback: return truncated hash
    return truncateNodeId(nodeId);
}

/**
 * Get node ID prefix for a friendly name
 */
export function getNodeIdByName(name: string): string | undefined {
    return NAME_TO_ID_PREFIX[name];
}

/**
 * Truncate a node ID for display (e.g., "0x25a3...156c")
 */
export function truncateNodeId(nodeId: string, prefixLen: number = 6, suffixLen: number = 4): string {
    if (!nodeId) return "Unknown";
    if (nodeId.length <= prefixLen + suffixLen + 3) return nodeId;
    return `${nodeId.slice(0, prefixLen)}...${nodeId.slice(-suffixLen)}`;
}

/**
 * Get all configured validator nodes
 */
export function getAllValidatorConfigs(): ValidatorNameConfig[] {
    return Object.entries(NAME_TO_ID_PREFIX).map(([name, shortId]) => ({
        id: shortId,
        name,
        shortId: shortId.slice(0, 12),
    }));
}

/**
 * Check if a node ID matches a known validator
 */
export function isKnownValidator(nodeId: string): boolean {
    return getNodeName(nodeId) !== truncateNodeId(nodeId);
}
