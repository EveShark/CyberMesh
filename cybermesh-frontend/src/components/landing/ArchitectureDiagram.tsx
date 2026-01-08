import { useState } from "react";

interface LayerInfo {
  id: string;
  label: string;
  description: string;
  y: number;
}

const layers: LayerInfo[] = [
  {
    id: "detection",
    label: "Autonomous AI Detection",
    description: "Real-time network anomaly detection at machine speed. No human latency.",
    y: 50,
  },
  {
    id: "verification",
    label: "Multi-Agent Verification",
    description: "Distributed analysis eliminates false positives. Every signal is cross-validated.",
    y: 120,
  },
  {
    id: "consensus",
    label: "Consensus-Verified Integrity",
    description: "Tamper-proof decisions verified by decentralized consensus. Uncensorable.",
    y: 190,
  },
];

const ArchitectureDiagram = () => {
  const [activeLayer, setActiveLayer] = useState<string | null>(null);

  // Generate mesh nodes for each layer
  const generateNodes = (layerY: number, count: number, layerId: string) => {
    const nodes = [];
    const spacing = 340 / (count + 1);
    
    for (let i = 1; i <= count; i++) {
      nodes.push({
        x: spacing * i + 40,
        y: layerY,
        id: `${layerId}-node-${i}`,
      });
    }
    return nodes;
  };

  const layer1Nodes = generateNodes(50, 5, "detection");
  const layer2Nodes = generateNodes(120, 4, "verification");
  const layer3Nodes = generateNodes(190, 3, "consensus");

  // Generate connections between layers
  const generateConnections = () => {
    const connections: { x1: number; y1: number; x2: number; y2: number; key: string }[] = [];
    
    layer1Nodes.forEach((n1, i) => {
      layer2Nodes.forEach((n2, j) => {
        if (Math.abs(i - j) <= 1 || (i === 0 && j === 0) || (i === layer1Nodes.length - 1 && j === layer2Nodes.length - 1)) {
          connections.push({ x1: n1.x, y1: n1.y, x2: n2.x, y2: n2.y, key: `l1-l2-${i}-${j}` });
        }
      });
    });

    layer2Nodes.forEach((n2, i) => {
      layer3Nodes.forEach((n3, j) => {
        if (Math.abs(i - j) <= 1) {
          connections.push({ x1: n2.x, y1: n2.y, x2: n3.x, y2: n3.y, key: `l2-l3-${i}-${j}` });
        }
      });
    });

    return connections;
  };

  const connections = generateConnections();

  const isLayerActive = (layerId: string) => activeLayer === layerId;

  const getNodeColor = (layerId: string) => {
    if (isLayerActive(layerId)) {
      return "hsl(var(--ember))";
    }
    return "hsl(var(--cyber-surface))";
  };

  const getStrokeColor = (layerId: string) => {
    if (isLayerActive(layerId)) {
      return "hsl(var(--ember))";
    }
    return "hsl(var(--ember) / 0.5)";
  };

  return (
    <div className="relative w-full max-w-md mx-auto">
      <svg
        viewBox="0 0 420 240"
        className="w-full h-auto"
        style={{ filter: "drop-shadow(0 0 10px hsl(var(--ember) / 0.1))" }}
      >
        {/* Connections */}
        <g className="transition-all duration-300">
          {connections.map((conn) => (
            <line
              key={conn.key}
              x1={conn.x1}
              y1={conn.y1}
              x2={conn.x2}
              y2={conn.y2}
              className={`transition-all duration-300 ${
                activeLayer ? "opacity-20" : "opacity-30"
              }`}
              stroke="hsl(var(--ember) / 0.4)"
              strokeWidth="1"
            />
          ))}
        </g>

        {/* Layer regions (invisible but hoverable) */}
        {layers.map((layer) => (
          <g key={layer.id}>
            <rect
              x="30"
              y={layer.y - 25}
              width="360"
              height="50"
              fill="transparent"
              className="cursor-pointer"
              onMouseEnter={() => setActiveLayer(layer.id)}
              onMouseLeave={() => setActiveLayer(null)}
            />
          </g>
        ))}

        {/* Nodes for Layer 1 */}
        {layer1Nodes.map((node) => (
          <g key={node.id} className="transition-all duration-300">
            <circle
              cx={node.x}
              cy={node.y}
              r={isLayerActive("detection") ? 6 : 5}
              fill={getNodeColor("detection")}
              stroke={getStrokeColor("detection")}
              strokeWidth="1.5"
              className={`transition-all duration-300 ${isLayerActive("detection") ? "animate-pulse-glow" : ""}`}
              style={{
                filter: isLayerActive("detection") ? "drop-shadow(0 0 5px hsl(var(--ember)))" : "none",
              }}
            />
          </g>
        ))}

        {/* Nodes for Layer 2 */}
        {layer2Nodes.map((node) => (
          <g key={node.id} className="transition-all duration-300">
            <circle
              cx={node.x}
              cy={node.y}
              r={isLayerActive("verification") ? 8 : 6}
              fill={getNodeColor("verification")}
              stroke={getStrokeColor("verification")}
              strokeWidth="1.5"
              className={`transition-all duration-300 ${isLayerActive("verification") ? "animate-pulse-glow" : ""}`}
              style={{
                filter: isLayerActive("verification") ? "drop-shadow(0 0 6px hsl(var(--ember)))" : "none",
              }}
            />
          </g>
        ))}

        {/* Nodes for Layer 3 */}
        {layer3Nodes.map((node) => (
          <g key={node.id} className="transition-all duration-300">
            <circle
              cx={node.x}
              cy={node.y}
              r={isLayerActive("consensus") ? 10 : 8}
              fill={getNodeColor("consensus")}
              stroke={getStrokeColor("consensus")}
              strokeWidth="1.5"
              className={`transition-all duration-300 ${isLayerActive("consensus") ? "animate-pulse-glow" : ""}`}
              style={{
                filter: isLayerActive("consensus") ? "drop-shadow(0 0 8px hsl(var(--ember)))" : "none",
              }}
            />
          </g>
        ))}

        {/* Layer Labels */}
        {layers.map((layer) => (
          <text
            key={`label-${layer.id}`}
            x="210"
            y={layer.y + 30}
            textAnchor="middle"
            className={`text-[9px] font-medium transition-all duration-300 ${
              isLayerActive(layer.id) ? "fill-ember" : "fill-muted-foreground"
            }`}
          >
            {layer.label}
          </text>
        ))}
      </svg>

      {/* Tooltip */}
      <div
        className={`absolute left-1/2 -translate-x-1/2 -bottom-1 w-full max-w-xs px-3 transition-all duration-300 ${
          activeLayer ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2 pointer-events-none"
        }`}
      >
        <div className="bento-card rounded-lg p-2.5 text-center border-ember/20">
          <p className="text-[10px] text-foreground">
            {layers.find((l) => l.id === activeLayer)?.description}
          </p>
        </div>
      </div>
    </div>
  );
};

export default ArchitectureDiagram;
