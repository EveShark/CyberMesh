# 🛡️ CyberMesh

> Real-time blockchain security monitoring & threat detection dashboard

## 🚀 Overview

CyberMesh is a comprehensive security monitoring platform for blockchain networks. It provides real-time visibility into ledger activity, validator consensus, threat detection, and AI-powered anomaly identification.

## ✨ Features

| Module | Description |
|--------|-------------|
| **📊 Dashboard** | Overview of all security metrics, alerts, and system status |
| **🤖 AI Engine** | Machine learning-powered threat detection with real-time anomaly scoring |
| **⛓️ Blockchain** | Ledger monitoring, block explorer, and transaction timeline |
| **🌐 Network** | Validator status, consensus tracking, and network topology |
| **🔒 Threats** | Threat volume analysis, severity distribution, and detection breakdown |
| **💚 System Health** | Infrastructure monitoring, uptime tracking, and service health |
| **⚙️ Settings** | Demo mode toggle, connection status, and configuration |

## 🛠️ Tech Stack

- **Framework**: React 18 + TypeScript
- **Build Tool**: Vite
- **Styling**: Tailwind CSS + shadcn/ui
- **Backend**: Supabase Edge Functions → Go Backend (GKE)
- **State Management**: TanStack Query
- **Routing**: React Router v6
- **Charts**: Recharts

## 📁 Project Structure

```
src/
├── components/
│   ├── features/        # Feature-specific components
│   │   ├── dashboard/
│   │   ├── ai-engine/
│   │   ├── blockchain/
│   │   ├── network/
│   │   ├── threats/
│   │   └── system-health/
│   ├── landing/         # Landing page components
│   ├── layout/          # Layout components (Sidebar, DashboardLayout)
│   └── ui/              # shadcn/ui primitives
├── config/              # App configuration & constants
├── hooks/               # Custom React hooks
├── integrations/        # External service integrations
├── lib/                 # Utility functions & API client
├── mocks/               # Mock data for demo mode
├── pages/               # Route page components
└── types/               # TypeScript type definitions
```

## 🏃 Getting Started

### Prerequisites
- Node.js 18+ or Bun
- npm, yarn, or bun

### Installation

```bash
# Clone the repository
git clone <YOUR_GIT_URL>
cd cybermesh

# Install dependencies
npm install

# Start development server
npm run dev
```

The app will be available at `http://localhost:8080`

## 🧭 Routes

| Path | Page | Description |
|------|------|-------------|
| `/` | Landing | Marketing/landing page |
| `/dashboard` | Dashboard | Main overview |
| `/ai-engine` | AI Engine | Threat detection AI |
| `/blockchain` | Blockchain | Ledger explorer |
| `/network` | Network | Validator consensus |
| `/threats` | Threats | Threat analysis |
| `/system-health` | System Health | Infrastructure status |
| `/settings` | Settings | Application configuration |

## 📱 Mobile Experience

CyberMesh is optimized for mobile with native-feeling interactions:

| Feature | Description |
|---------|-------------|
| **Swipe Gestures** | Swipe from left edge to open sidebar, swipe left anywhere to close |
| **Bottom Navigation** | Fixed bottom nav bar with all 6 main sections for quick thumb access |
| **Auto-close Sidebar** | Sidebar automatically closes when navigating to a new page |
| **Hidden Scrollbars** | Clean mobile interface with invisible scrollbars |
| **Settings Gear** | Access settings via gear icon in header (mobile) |
| **Pull-to-Refresh** | Pull down on any dashboard page to refresh data |

## 🔄 Demo Mode

CyberMesh includes a demo mode for testing without a backend:

| Method | Description |
|--------|-------------|
| **Environment Variable** | Set `VITE_DEMO_MODE=true` in `.env` |
| **Runtime Toggle** | Use the Settings page (`/settings`) to switch modes |

When demo mode is enabled:
- Mock data is used (no API calls)
- Connection indicator shows "Demo" (purple)
- Polling is disabled
- Instant data loading

## 🎣 Custom Hooks

| Hook | Purpose |
|------|---------|
| `useSwipe` | Detects touch swipe gestures with configurable threshold and edge detection |
| `useMobile` | Returns boolean for mobile device detection |
| `useToast` | Toast notification management |
| `useConnectionStatus` | Monitors backend connection status |
| `useAdaptivePolling` | Adjusts polling interval based on visibility and network |
| `usePullToRefresh` | Implements pull-to-refresh gesture for mobile |
| `useDashboardData` | Fetches dashboard data (auto-handles demo mode) |
| `useThreatsData` | Fetches threats data (auto-handles demo mode) |
| `useNetworkData` | Fetches network data (auto-handles demo mode) |
| `useBlockchainData` | Fetches blockchain data (auto-handles demo mode) |
| `useAIEngineData` | Fetches AI engine data (auto-handles demo mode) |
| `useSystemHealthData` | Fetches system health data (auto-handles demo mode) |

## 🎨 Design System

CyberMesh uses a dark-themed glassmorphic design with:
- **Primary**: Cyan/Teal accents (frost)
- **Alerts**: Amber for warnings, Rose for critical (fire)
- **Glass Effects**: `glass-frost`, `frost-glow` utilities
- **Gradients**: `text-gradient`, `text-gradient-fire`
- **Animations**: `pulse-slow`, `pulse-glow`, `fade-in-up`
- **Connection Status**: Green (connected), Amber (connecting), Red (disconnected), Purple (demo)

## 📚 Documentation

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | Frontend architecture and design decisions |
| [API.md](docs/API.md) | API endpoint documentation |
| [DEVELOPER_INTEGRATION_GUIDE.md](docs/DEVELOPER_INTEGRATION_GUIDE.md) | Backend integration guide |
| [CONTACT_FORM_SETUP.md](docs/CONTACT_FORM_SETUP.md) | Contact form email setup |
| [CHANGELOG.md](docs/CHANGELOG.md) | Version history and changes |

## 📝 License

MIT License
