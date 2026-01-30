# CyberMesh Frontend - Low-Level Design (LLD)

**Version:** 2.0.0  
**Last Updated:** 2026-01-30

---

## 📑 Navigation

**Quick Links:**
- [🏗️ Project Structure](#3-project-structure)
- [📄 Page Architecture](#4-page-architecture)
- [🧩 Components](#5-component-hierarchy)
- [📊 Data Fetching](#6-data-fetching-srclib)
- [📈 Charts](#8-chart-components)

---

## 1. Overview

The Frontend is a **React/TypeScript dashboard** for monitoring CyberMesh threat detection and network status.

> [!NOTE]
> The frontend uses **React Query** for server state management with automatic refetching and caching.

---

## 2. Technology Stack

| Layer | Technology |
|-------|------------|
| Framework | React 18 ✅ |
| Language | TypeScript |
| Build | Vite ⚡ |
| Styling | TailwindCSS 🎨 |
| State | React Query |
| Charts | Recharts 📊 |
| HTTP | fetch (typed client) |

---

## 3. Project Structure

```mermaid
graph TB
    subgraph src["src/"]
        pages[pages/]
        components[components/]
        hooks[hooks/]
        lib[lib/]
        types[types/]
        config[config/]
    end
    
    pages --> components
    pages --> hooks
    components --> hooks
    hooks --> lib
    lib --> types
    
    classDef folder fill:#e3f2fd,stroke:#1565c0,color:#000;
    
    class pages,components,hooks,lib,types,config folder;
```

---

## 4. Page Architecture

### 4.1 Routes

| Path | Page | Description |
|------|------|-------------|
| `/` | Index | Landing / entry |
| `/dashboard` | Dashboard | Overview metrics 📊 |
| `/ai-engine` | AI Engine | AI engine status 🤖 |
| `/threats` | Threats | Threat list 🚨 |
| `/blockchain` | Blockchain | Ledger / block views ⛓️ |
| `/network` | Network | Network topology 🌐 |
| `/system-health` | System Health | Health + readiness ✅ |
| `/settings` | Settings | Configuration ⚙️ |
| `/404` | NotFound | Not found ❌ |

### 4.2 Page Flow

```mermaid
flowchart TB
    APP[App.tsx] --> ROUTER[React Router]
    ROUTER --> DASH[Dashboard]
    ROUTER --> NET[Network]
    ROUTER --> THR[Threats]
    ROUTER --> VAL[Validators]
    
    DASH --> OVER[OverviewCards]
    DASH --> CHART[DetectionChart]
    DASH --> LIST[RecentThreats]
    
    classDef app fill:#61dafb,stroke:#000,color:#000;
    classDef page fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef component fill:#fff9c4,stroke:#f57f17,color:#000;
    
    class APP app;
    class DASH,NET,THR,VAL page;
    class OVER,CHART,LIST component;
```

---

## 5. Component Hierarchy

### 5.1 Dashboard Components

```mermaid
classDiagram
    class Dashboard {
        +render()
    }
    
    class DashboardHeader
    class StatusCard
    class MetricCard
    class InfrastructureCard
    class AlertsCard
    class LatestBlocksTable
    
    Dashboard --> DashboardHeader
    Dashboard --> StatusCard
    Dashboard --> MetricCard
    Dashboard --> InfrastructureCard
    Dashboard --> AlertsCard
    Dashboard --> LatestBlocksTable
```

---

## 6. Data Fetching (`src/lib/`)

### 6.1 API Client

```mermaid
classDiagram
    class ApiClient {
        -baseURL string
        +get(path, signal) Promise
        +post(path, data, signal) Promise
    }
    
    class RateLimitedFetch {
        +rateLimitedFetch(url, opts) Promise
    }

    ApiClient --> RateLimitedFetch
```

### 6.2 React Query Hooks

```typescript
// Example: useDashboardData hook
export const useDashboardData = ({ pollingInterval = 15000 } = {}) =>
  useQuery({
    queryKey: ["dashboard-overview"],
    queryFn: ({ signal }) => apiClient.dashboard.getOverview(signal),
    refetchInterval: pollingInterval,
    staleTime: 10_000,
  });
```

---

## 7. State Management

### 7.1 Server State (React Query)

| Hook | Key | Refetch Interval |
|------|-----|------------------|
| `useDashboardData` | `['dashboard-overview']` | 30s (Dashboard) |
| `useThreatsData` | `['threats-summary']` | 15s |
| `useNetworkData` | `['network-status']` | 15s |
| `useBlockchainData` | `['blockchain-data', limit]` | 30s |
| `useAIEngineData` | `['ai-engine-status']` | 15s |
| `useSystemHealthData` | `['system-health']` | 15s |

### 7.2 UI State (React Context)

```mermaid
classDiagram
    class SidebarContext {
        +collapsed bool
        +toggle()
    }
```

---

## 8. Chart Components

### 8.1 Recharts-Based Charts

```mermaid
flowchart LR
    DATA[API Data] --> TRANSFORM[Transform]
    TRANSFORM --> RECHARTS[Recharts]
    RECHARTS --> SVG[SVG Render]
    
    subgraph Examples
        TS[ThreatSeverityChart]
        TV[ThreatVolumeChart]
        VC[Validator Charts]
    end
    
    style RECHARTS fill:#c8e6c9,stroke:#2e7d32,color:#000;
```

---

## 9. API Endpoints Used

| Endpoint | Method | Component |
|----------|--------|-----------|
| `/api/v1/dashboard/overview` | GET | Dashboard + derived pages |
| `/api/v1/ready` | GET | BackendStatusPanel / SystemHealth |
| `/api/v1/health` | GET | SystemHealth |
| `/api/v1/blocks?limit=N` | GET | Blockchain page (optional) |

---

## 10. Key Files

| File | Purpose |
|------|---------|
| `src/App.tsx` | Root component |
| `src/pages/Dashboard.tsx` | Dashboard page |
| `src/pages/Network.tsx` | Network page |
| `src/pages/Threats.tsx` | Threats page |
| `src/pages/Blockchain.tsx` | Blockchain page |
| `src/pages/AIEngine.tsx` | AI engine page |
| `src/pages/SystemHealth.tsx` | System health page |
| `src/lib/api/client.ts` | Typed API client |
| `src/lib/api/rate-limiter.ts` | Retry/backoff wrapper |
| `src/lib/query-client.ts` | React Query client config |
| `src/config/runtime.ts` | Runtime config loader |

---

## 11. Related Documents

### Design Documents
- [HLD](./HLD.md) - High-level design

### Source Code
- [Frontend README](../../cybermesh-frontend/README.md)

---

**[⬆️ Back to Top](#-navigation)**
