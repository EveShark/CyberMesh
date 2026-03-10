import React, { useState, useEffect, useMemo } from 'react';
import { Loader2, CheckCircle, RefreshCw, XCircle, Crown, Shield, TrendingUp, Zap, Activity, Clock } from 'lucide-react';

// Mock data generator
const generateMockData = () => {
  const statuses = ['healthy', 'healthy', 'healthy', 'warning', 'critical'];
  const nodes = Array.from({ length: 5 }, (_, i) => ({
    id: `node-${i + 1}`,
    name: `Validator-${i + 1}`,
    status: statuses[i],
    latency: Math.floor(Math.random() * 100) + 20,
    lastSeen: new Date(Date.now() - Math.random() * 300000).toISOString(),
    uptime: 95 + Math.random() * 5,
    messagesProcessed: Math.floor(Math.random() * 1000) + 500
  }));

  return { nodes, leaderId: 'node-1', leaderStability: 98.5 };
};

// Network Visualization Component - FULLY RESPONSIVE
function NetworkVisualization({ nodes, leaderId, isLoading }) {
  const [selectedNode, setSelectedNode] = useState(null);
  const [hoveredNode, setHoveredNode] = useState(null);
  const [isMobile, setIsMobile] = useState(false);
  const canvasRef = React.useRef(null);
  const containerRef = React.useRef(null);

  const validators = nodes.slice(0, 5);

  useEffect(() => {
    const checkMobile = () => setIsMobile(window.innerWidth < 768);
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  // Draw mesh connections on canvas
  useEffect(() => {
    if (!canvasRef.current || !containerRef.current || isMobile) return;

    const canvas = canvasRef.current;
    const ctx = canvas.getContext('2d');
    const container = containerRef.current;
    
    const updateCanvas = () => {
      const rect = container.getBoundingClientRect();
      canvas.width = rect.width;
      canvas.height = rect.height;

      ctx.clearRect(0, 0, canvas.width, canvas.height);

      const centerX = canvas.width / 2;
      const centerY = canvas.height / 2;
      const radiusPercent = 38;
      const radiusX = (canvas.width / 100) * radiusPercent;
      const radiusY = (canvas.height / 100) * radiusPercent * 0.75;

      // Calculate node positions
      const nodePositions = validators.map((node, index) => {
        const angle = (index * 72 - 90) * (Math.PI / 180);
        return {
          id: node.id,
          x: centerX + radiusX * Math.cos(angle),
          y: centerY + radiusY * Math.sin(angle),
          status: node.status
        };
      });

      // Draw ring connections (node to next node)
      ctx.lineWidth = 2;
      nodePositions.forEach((node, index) => {
        const nextNode = nodePositions[(index + 1) % nodePositions.length];
        const isHighlighted = hoveredNode === node.id || hoveredNode === nextNode.id;
        
        // Check if either node is critical for dashed line
        const isCritical = node.status === 'critical' || nextNode.status === 'critical';
        
        ctx.strokeStyle = isHighlighted ? 'rgba(96, 165, 250, 0.6)' : 'rgba(148, 163, 184, 0.3)';
        ctx.beginPath();
        ctx.moveTo(node.x, node.y);
        ctx.lineTo(nextNode.x, nextNode.y);
        
        if (isCritical) {
          ctx.setLineDash([8, 8]);
        } else {
          ctx.setLineDash([]);
        }
        
        ctx.stroke();
      });

      ctx.setLineDash([]);
    };

    updateCanvas();
    window.addEventListener('resize', updateCanvas);
    return () => window.removeEventListener('resize', updateCanvas);
  }, [isMobile, hoveredNode, validators]);

  const getStatusColor = (status) => {
    const s = status?.toLowerCase() || '';
    if (s === 'healthy') return { bg: 'bg-emerald-500', glow: 'shadow-emerald-500/50', text: 'text-emerald-500' };
    if (s === 'warning') return { bg: 'bg-amber-500', glow: 'shadow-amber-500/50', text: 'text-amber-500' };
    return { bg: 'bg-red-500', glow: 'shadow-red-500/50', text: 'text-red-500' };
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[300px] md:min-h-[500px]">
        <div className="text-center space-y-3">
          <Loader2 className="h-8 w-8 animate-spin text-blue-400 mx-auto" />
          <p className="text-sm text-slate-400">Initializing network...</p>
        </div>
      </div>
    );
  }

  // Mobile: Grid Layout
  if (isMobile) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 gap-3">
          {validators.map((node) => {
            const colors = getStatusColor(node.status);
            const isLeader = leaderId === node.id;
            const isSelected = selectedNode?.id === node.id;

            return (
              <div
                key={node.id}
                onClick={() => setSelectedNode(selectedNode?.id === node.id ? null : node)}
                className={`p-4 rounded-xl backdrop-blur-xl border transition-all duration-300 cursor-pointer ${
                  isSelected 
                    ? 'border-blue-400/50 bg-slate-800/90 shadow-xl' 
                    : 'border-white/10 bg-slate-800/60 shadow-lg'
                }`}
              >
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-2">
                    {isLeader && <Crown className="h-5 w-5 text-amber-400" />}
                    <div>
                      <p className="text-sm font-semibold text-white">{node.name}</p>
                      <p className="text-xs text-slate-400">{node.id}</p>
                    </div>
                  </div>
                  <div className={`w-3 h-3 rounded-full ${colors.bg} ${colors.glow} shadow-lg animate-pulse`}></div>
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <p className="text-xs text-slate-400">Latency</p>
                    <p className={`text-lg font-bold ${colors.text}`}>{node.latency}ms</p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-400">Uptime</p>
                    <p className="text-lg font-bold text-white">{node.uptime?.toFixed(1)}%</p>
                  </div>
                </div>

                {isSelected && (
                  <div className="mt-3 pt-3 border-t border-white/10">
                    <div className="grid grid-cols-2 gap-2 text-xs">
                      <div>
                        <p className="text-slate-400">Messages</p>
                        <p className="text-white font-semibold">{node.messagesProcessed}</p>
                      </div>
                      <div>
                        <p className="text-slate-400">Status</p>
                        <p className={`font-semibold ${colors.text}`}>{node.status}</p>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    );
  }

  // Desktop: Circular Layout (Responsive)
  return (
    <div className="relative">
      <div ref={containerRef} className="relative w-full" style={{ paddingBottom: '75%' }}>
        {/* Canvas for mesh connections */}
        <canvas
          ref={canvasRef}
          className="absolute inset-0 pointer-events-none"
        />
        
        <div className="absolute inset-0 flex items-center justify-center">
          {/* Center Hub - Shows consensus state, not connected */}
          <div className="relative z-10">
            <div className="w-20 h-20 lg:w-32 lg:h-32 rounded-full bg-gradient-to-br from-blue-500/20 to-purple-500/20 backdrop-blur-xl border border-white/10 flex items-center justify-center">
              <div className="text-center">
                <Zap className="h-5 w-5 lg:h-8 lg:w-8 text-blue-400 mx-auto mb-1" />
                <p className="text-[10px] lg:text-xs text-slate-400 font-medium">Consensus</p>
              </div>
            </div>
            <div className="absolute inset-0 rounded-full bg-blue-500/20 animate-ping"></div>
          </div>

          {/* Validator Nodes - Ring Topology */}
          {validators.map((node, index) => {
            const colors = getStatusColor(node.status);
            const isLeader = leaderId === node.id;
            const isHovered = hoveredNode === node.id;
            const isSelected = selectedNode?.id === node.id;

            // Responsive radius: 30-45% of container width
            const radiusPercent = 38;
            const angle = (index * 72 - 90) * (Math.PI / 180);

            return (
              <div
                key={node.id}
                className="absolute z-20"
                style={{
                  left: '50%',
                  top: '50%',
                  transform: `translate(-50%, -50%) translate(${Math.cos(angle) * radiusPercent}vw, ${Math.sin(angle) * radiusPercent * 0.75}vw)`,
                }}
                onMouseEnter={() => setHoveredNode(node.id)}
                onMouseLeave={() => setHoveredNode(null)}
                onClick={() => setSelectedNode(selectedNode?.id === node.id ? null : node)}
              >
                <div className={`transition-all duration-300 cursor-pointer ${isHovered || isSelected ? 'scale-110' : 'scale-100'}`}>
                  {isLeader && (
                    <div className="absolute -top-6 lg:-top-8 left-1/2 -translate-x-1/2 animate-bounce">
                      <Crown className="h-4 w-4 lg:h-6 lg:w-6 text-amber-400 drop-shadow-lg" />
                    </div>
                  )}

                  {(isHovered || isSelected) && (
                    <div className={`absolute inset-0 rounded-xl lg:rounded-2xl blur-xl ${colors.bg} opacity-40 animate-pulse`}></div>
                  )}

                  <div className={`relative w-28 h-28 lg:w-40 lg:h-40 rounded-xl lg:rounded-2xl backdrop-blur-xl border transition-all duration-300 ${
                    isSelected 
                      ? 'border-blue-400/50 bg-slate-800/90 shadow-2xl shadow-blue-500/20' 
                      : isHovered
                      ? 'border-white/30 bg-slate-800/80 shadow-xl'
                      : 'border-white/10 bg-slate-800/60 shadow-lg'
                  }`}>
                    <div className={`absolute -top-1.5 -right-1.5 lg:-top-2 lg:-right-2 w-5 h-5 lg:w-6 lg:h-6 rounded-full ${colors.bg} shadow-lg ${colors.glow} flex items-center justify-center`}>
                      <div className="w-2 h-2 lg:w-3 lg:h-3 bg-white rounded-full animate-pulse"></div>
                    </div>

                    <div className="p-2.5 lg:p-4 h-full flex flex-col justify-between">
                      <div>
                        <p className="text-xs lg:text-sm font-semibold text-white mb-0.5 lg:mb-1 truncate">{node.name}</p>
                        <p className="text-[10px] lg:text-xs text-slate-400 truncate">{node.id}</p>
                      </div>

                      <div className="space-y-1 lg:space-y-2">
                        <div className="flex items-center justify-between text-[10px] lg:text-xs">
                          <span className="text-slate-400">Latency</span>
                          <span className={`font-mono font-semibold ${colors.text}`}>
                            {node.latency}ms
                          </span>
                        </div>
                        <div className="flex items-center justify-between text-[10px] lg:text-xs">
                          <span className="text-slate-400">Uptime</span>
                          <span className="font-semibold text-white">
                            {node.uptime?.toFixed(1)}%
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Selected Node Details */}
      {selectedNode && (
        <div className="mt-6 p-4 lg:p-6 rounded-xl backdrop-blur-xl bg-slate-800/60 border border-white/10 shadow-2xl animate-in fade-in slide-in-from-bottom-4 duration-300">
          <div className="flex items-start justify-between mb-4">
            <div>
              <h3 className="text-base lg:text-lg font-semibold text-white flex items-center gap-2">
                {selectedNode.id === leaderId && <Crown className="h-4 w-4 lg:h-5 lg:w-5 text-amber-400" />}
                {selectedNode.name}
              </h3>
              <p className="text-xs lg:text-sm text-slate-400">{selectedNode.id}</p>
            </div>
            <button
              onClick={() => setSelectedNode(null)}
              className="text-slate-400 hover:text-white transition-colors"
            >
              <XCircle className="h-5 w-5" />
            </button>
          </div>

          <div className="grid grid-cols-3 gap-3 lg:gap-4">
            <div className="p-2.5 lg:p-3 rounded-lg bg-slate-700/50 border border-slate-600/30">
              <div className="flex items-center gap-1.5 lg:gap-2 mb-1">
                <Activity className="h-3.5 w-3.5 lg:h-4 lg:w-4 text-emerald-400" />
                <p className="text-[10px] lg:text-xs text-slate-400">Messages</p>
              </div>
              <p className="text-base lg:text-xl font-bold text-white">{selectedNode.messagesProcessed}</p>
            </div>
            <div className="p-2.5 lg:p-3 rounded-lg bg-slate-700/50 border border-slate-600/30">
              <div className="flex items-center gap-1.5 lg:gap-2 mb-1">
                <Zap className="h-3.5 w-3.5 lg:h-4 lg:w-4 text-blue-400" />
                <p className="text-[10px] lg:text-xs text-slate-400">Latency</p>
              </div>
              <p className="text-base lg:text-xl font-bold text-white">{selectedNode.latency}ms</p>
            </div>
            <div className="p-2.5 lg:p-3 rounded-lg bg-slate-700/50 border border-slate-600/30">
              <div className="flex items-center gap-1.5 lg:gap-2 mb-1">
                <Clock className="h-3.5 w-3.5 lg:h-4 lg:w-4 text-purple-400" />
                <p className="text-[10px] lg:text-xs text-slate-400">Uptime</p>
              </div>
              <p className="text-base lg:text-xl font-bold text-white">{selectedNode.uptime?.toFixed(2)}%</p>
            </div>
          </div>
        </div>
      )}

      {/* Legend */}
      <div className="mt-6 p-4 lg:p-6 rounded-xl backdrop-blur-xl bg-slate-800/60 border border-white/10 shadow-xl">
        <h3 className="text-sm lg:text-base font-semibold text-white mb-4 flex items-center gap-2">
          <Shield className="h-4 w-4 lg:h-5 lg:w-5 text-blue-400" />
          Legend
        </h3>
        
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 lg:gap-6">
          {/* Status Indicators */}
          <div className="space-y-3">
            <p className="text-xs font-semibold text-slate-300 uppercase tracking-wide">Node Status</p>
            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <div className="w-4 h-4 rounded-full bg-emerald-500 shadow-lg shadow-emerald-500/50 flex items-center justify-center">
                  <div className="w-2 h-2 bg-white rounded-full"></div>
                </div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Healthy</p>
                  <p className="text-xs text-slate-400">Node operating normally</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-4 h-4 rounded-full bg-amber-500 shadow-lg shadow-amber-500/50 flex items-center justify-center">
                  <div className="w-2 h-2 bg-white rounded-full"></div>
                </div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Warning</p>
                  <p className="text-xs text-slate-400">Node experiencing issues</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-4 h-4 rounded-full bg-red-500 shadow-lg shadow-red-500/50 flex items-center justify-center">
                  <div className="w-2 h-2 bg-white rounded-full"></div>
                </div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Critical</p>
                  <p className="text-xs text-slate-400">Node failing or offline</p>
                </div>
              </div>
            </div>
          </div>

          {/* Network Elements */}
          <div className="space-y-3">
            <p className="text-xs font-semibold text-slate-300 uppercase tracking-wide">Network Elements</p>
            <div className="space-y-2">
              <div className="flex items-center gap-3">
                <Crown className="h-5 w-5 text-amber-400 flex-shrink-0" />
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Leader Node</p>
                  <p className="text-xs text-slate-400">Current consensus leader</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-12 h-0.5 bg-slate-400 flex-shrink-0"></div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Active Connection</p>
                  <p className="text-xs text-slate-400">Healthy node-to-node link</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-12 h-0.5 border-t-2 border-dashed border-slate-400 flex-shrink-0"></div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Degraded Connection</p>
                  <p className="text-xs text-slate-400">Link involving critical node</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-5 h-5 rounded-full bg-gradient-to-br from-blue-500/30 to-purple-500/30 border border-white/20 flex-shrink-0"></div>
                <div className="flex-1">
                  <p className="text-sm text-white font-medium">Consensus Hub</p>
                  <p className="text-xs text-slate-400">Central coordination point</p>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Interaction Tips */}
        <div className="mt-4 pt-4 border-t border-white/10">
          <p className="text-xs font-semibold text-slate-300 uppercase tracking-wide mb-2">Interactions</p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs text-slate-400">
            <div className="flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-blue-400"></div>
              <span>Hover over nodes to highlight connections</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-blue-400"></div>
              <span>Click nodes to view detailed metrics</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// Validator Status Table Component (Already responsive, minor tweaks)
function ValidatorStatusTable({ nodes, leaderId, leaderStability, isLoading }) {
  const validators = useMemo(() => {
    return nodes.slice(0, 5).map((node) => ({
      ...node,
      role: 'validator',
      isLeader: leaderId ? node.id === leaderId : false,
    }));
  }, [nodes, leaderId]);

  const networkHealth = useMemo(() => {
    const activeNodes = validators.filter((v) => {
      const status = v.status?.toLowerCase() || '';
      return status === 'healthy' || status === 'warning';
    }).length;

    const total = validators.length;
    const canLose = Math.floor((total - 1) / 3);

    return {
      activeNodes,
      total,
      percentage: total > 0 ? ((activeNodes / total) * 100).toFixed(0) : 0,
      canLose,
      riskLevel: activeNodes >= Math.ceil((2 * total + 1) / 3) ? 'LOW' : 'HIGH',
    };
  }, [validators]);

  const formatLastSeen = (lastSeen) => {
    if (!lastSeen) return '--';
    const date = new Date(lastSeen);
    if (isNaN(date.getTime())) return '--';

    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);

    if (diffSec < 60) return `${diffSec}s ago`;
    if (diffMin < 60) return `${diffMin}m ago`;
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const getStatusBadge = (status) => {
    const s = status?.toLowerCase() || '';
    if (s === 'healthy') {
      return (
        <span className="inline-flex items-center gap-1 px-2 lg:px-2.5 py-1 rounded-full text-[10px] lg:text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
          <CheckCircle className="h-3 w-3" /> Healthy
        </span>
      );
    }
    if (s === 'warning') {
      return (
        <span className="inline-flex items-center gap-1 px-2 lg:px-2.5 py-1 rounded-full text-[10px] lg:text-xs font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20">
          <RefreshCw className="h-3 w-3" /> Warning
        </span>
      );
    }
    return (
      <span className="inline-flex items-center gap-1 px-2 lg:px-2.5 py-1 rounded-full text-[10px] lg:text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
        <XCircle className="h-3 w-3" /> Critical
      </span>
    );
  };

  if (isLoading) {
    return (
      <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
        <div className="p-4 lg:p-6 border-b border-white/10">
          <h3 className="text-base lg:text-lg font-semibold text-white">Validator Status</h3>
          <p className="text-xs lg:text-sm text-slate-400">5-node cluster health</p>
        </div>
        <div className="p-4 lg:p-6">
          <div className="flex items-center justify-center min-h-[200px]">
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-blue-400 mx-auto" />
              <p className="text-sm text-slate-400">Loading validator status...</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="backdrop-blur-xl bg-slate-800/60 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl">
      <div className="p-4 lg:p-6 border-b border-white/10">
        <h3 className="text-base lg:text-lg font-semibold text-white">Validator Status</h3>
        <p className="text-xs lg:text-sm text-slate-400">5-node cluster health and Byzantine fault tolerance</p>
      </div>
      <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
        {/* Validator Cards Grid */}
        <div className="grid grid-cols-1 gap-2.5 lg:gap-3">
          {validators.map((validator) => (
            <div
              key={validator.id}
              className="p-3 lg:p-4 rounded-lg lg:rounded-xl bg-slate-700/30 border border-white/10 hover:border-white/20 hover:bg-slate-700/50 transition-all duration-200"
            >
              <div className="flex items-center justify-between flex-wrap gap-2">
                <div className="flex items-center gap-2 lg:gap-3 min-w-0 flex-1">
                  {validator.isLeader && <Crown className="h-4 w-4 lg:h-5 lg:w-5 text-amber-400 flex-shrink-0" />}
                  <div className="min-w-0">
                    <p className="font-semibold text-white text-sm lg:text-base truncate">{validator.name || validator.id}</p>
                    <p className="text-[10px] lg:text-xs text-slate-400">validator</p>
                  </div>
                </div>
                
                <div className="flex items-center gap-3 lg:gap-4">
                  {getStatusBadge(validator.status)}
                  <div className="text-right">
                    <p className="font-mono text-xs lg:text-sm font-semibold text-white">
                      {validator.latency !== undefined && validator.latency > 0
                        ? `${validator.latency.toFixed(0)}ms`
                        : '--'}
                    </p>
                    <p className="text-[10px] lg:text-xs text-slate-400">{formatLastSeen(validator.lastSeen)}</p>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>

        {/* Network Health Summary */}
        <div className="space-y-3 lg:space-y-4 pt-3 lg:pt-4 border-t border-white/10">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 lg:gap-4">
            <div className="p-3 lg:p-4 rounded-lg lg:rounded-xl bg-gradient-to-br from-emerald-500/10 to-emerald-500/5 border border-emerald-500/20">
              <p className="text-[10px] lg:text-xs text-emerald-400 mb-1 font-medium">Network Health</p>
              <p className="text-xl lg:text-2xl font-bold text-white">
                {networkHealth.activeNodes}/{networkHealth.total}
              </p>
              <p className="text-xs lg:text-sm text-slate-400">{networkHealth.percentage}% active nodes</p>
            </div>
            <div className="p-3 lg:p-4 rounded-lg lg:rounded-xl bg-gradient-to-br from-blue-500/10 to-blue-500/5 border border-blue-500/20">
              <p className="text-[10px] lg:text-xs text-blue-400 mb-1 font-medium">Leader Stability</p>
              <p className="text-xl lg:text-2xl font-bold text-white">
                {leaderStability !== undefined ? `${leaderStability.toFixed(1)}%` : '--'}
              </p>
              <p className="text-xs lg:text-sm text-slate-400">consensus maintained</p>
            </div>
          </div>

          <div className="p-3 lg:p-4 rounded-lg lg:rounded-xl bg-slate-700/30 border border-white/10 space-y-2 lg:space-y-3">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <span className="text-xs lg:text-sm text-slate-300 inline-flex items-center gap-2">
                <Shield className="h-3.5 w-3.5 lg:h-4 lg:w-4 text-blue-400" /> Byzantine Fault Tolerance
              </span>
              <span className={`px-2.5 lg:px-3 py-1 rounded-full text-[10px] lg:text-xs font-semibold ${
                networkHealth.riskLevel === 'LOW'
                  ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                  : 'bg-red-500/20 text-red-400 border border-red-500/30'
              }`}>
                {networkHealth.riskLevel === 'LOW' ? 'SAFE' : 'AT RISK'}
              </span>
            </div>
            <div className="flex items-center justify-between flex-wrap gap-2 text-xs lg:text-sm">
              <span className="text-slate-400 inline-flex items-center gap-2">
                <TrendingUp className="h-3.5 w-3.5 lg:h-4 lg:w-4" /> Consensus Risk
              </span>
              <span className="font-medium text-white">
                Can lose {networkHealth.canLose} more node{networkHealth.canLose !== 1 ? 's' : ''}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// Main Dashboard
export default function ValidatorDashboard() {
  const [data, setData] = useState(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    setTimeout(() => {
      setData(generateMockData());
      setIsLoading(false);
    }, 1500);

    const interval = setInterval(() => {
      setData(generateMockData());
    }, 3000);

    return () => clearInterval(interval);
  }, []);

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 p-4 lg:p-8">
      <div className="max-w-7xl mx-auto space-y-6 lg:space-y-8">
        {/* Header */}
        <div className="text-center space-y-2">
          <h1 className="text-3xl lg:text-5xl font-bold bg-gradient-to-r from-blue-400 via-purple-400 to-pink-400 bg-clip-text text-transparent">
            Validator Network Dashboard
          </h1>
          <p className="text-slate-400 text-sm lg:text-lg">
            Real-time monitoring of 5-node Byzantine consensus cluster
          </p>
        </div>

        {/* Network Visualization */}
        <div className="backdrop-blur-xl bg-slate-800/40 rounded-xl lg:rounded-2xl border border-white/10 shadow-2xl p-4 lg:p-8">
          <h2 className="text-base lg:text-xl font-semibold text-white mb-4 lg:mb-6 flex items-center gap-2">
            <Activity className="h-5 w-5 lg:h-6 lg:w-6 text-blue-400" />
            Network Topology
          </h2>
          <NetworkVisualization
            nodes={data?.nodes || []}
            leaderId={data?.leaderId}
            isLoading={isLoading}
          />
        </div>

        {/* Status Table */}
        <ValidatorStatusTable
          nodes={data?.nodes || []}
          leaderId={data?.leaderId}
          leaderStability={data?.leaderStability}
          isLoading={isLoading}
        />

        {/* Footer */}
        <div className="text-center pb-4">
          <div className="inline-flex items-center gap-2 px-3 lg:px-4 py-2 rounded-full bg-slate-800/60 border border-white/10 text-xs text-slate-400">
            <div className="w-2 h-2 bg-emerald-400 rounded-full animate-pulse"></div>
            Live • Updates every 3 seconds
          </div>
        </div>
      </div>
    </div>
  );
}