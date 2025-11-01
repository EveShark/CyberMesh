# CyberMesh Operator Dashboard (frontend-new)

**Modern Next.js Frontend with v0-generated Components**

This is a comprehensive operator dashboard for CyberMesh's distributed threat intelligence platform, built with:
- **Next.js 15** with App Router
- **TypeScript** for type safety
- **Tailwind CSS** for styling
- **shadcn/ui** components
- **v0.dev** generated components for rapid prototyping

---

## 🎯 What Was Installed

The dashboard includes **74 pre-built components** from the v0.app link, covering:

### 📊 Dashboard Pages
1. **Overview** (`/`) - System status and cluster health
2. **Threats** (`/threats`) - Real-time threat detection feed
3. **Consensus** (`/consensus`) - PBFT consensus visualization
4. **Blocks** (`/blocks`) - Blockchain explorer
5. **Ledger** (`/ledger`) - Transaction ledger
6. **Network** (`/network`) - P2P network topology
7. **AI Engine** (`/ai-engine`) - ML detection engine stats
8. **Metrics** (`/metrics`) - System performance metrics
9. **System Health** (`/system-health`) - Infrastructure monitoring
10. **Investors** (`/investors`) - Investor pitch deck view

### 🧩 Key Components

**UI Components** (`src/components/ui/`)
- `card`, `badge`, `button`, `table`, `sheet`, `tabs`
- `progress`, `input`, `label`, `alert`
- `dropdown-menu`, `separator`, `tooltip`, `skeleton`
- `chart`, `sidebar`

**Dashboard Components** (`src/components/`)
- `hero-kpi-strip` - Key performance indicators
- `node-status-grid` - 5-node cluster status
- `cluster-status-tiles` - Cluster health tiles
- `service-health-grid` - Service status
- `recent-threats-table` - Threat feed
- `pbft-live-visualizer` - Consensus animation
- `animated-network-graph` - P2P topology
- `detection-engines-grid` - ML engine stats
- `ai-engine-stats` - AI performance metrics
- `threat-charts` - Threat analytics
- `blocks-table` - Block explorer
- `sidebar-nav` - Navigation sidebar

### 🎨 Features

✅ **Dark mode** by default (ThemeProvider)
✅ **Investor mode** toggle (show/hide technical details)
✅ **Demo mode** for presentations
✅ **Responsive design** (mobile, tablet, desktop)
✅ **Real-time charts** using Recharts
✅ **Network visualization** with D3/custom animations
✅ **Type-safe API calls** with TypeScript

---

## 🚀 Quick Start

### 1. Install Dependencies
```bash
cd frontend-new
npm install
```

### 2. Configure Environment
Edit `.env.local` and set your backend URL:
```env
NEXT_PUBLIC_API_BASE=http://localhost:8443/api
BACKEND_API_BASE=http://localhost:8443/api
```

### 3. Run Development Server
```bash
npm run dev
```

Open [http://localhost:3000](http://localhost:3000) in your browser.

### 4. Build for Production
```bash
npm run build
npm start
```

---

## 📁 Project Structure

```
frontend-new/
├── src/
│   ├── app/                      # Next.js App Router pages
│   │   ├── page.tsx             # Homepage (Overview)
│   │   ├── layout.tsx           # Root layout with sidebar
│   │   ├── overview.tsx         # Overview page component
│   │   ├── threats/             # Threats page
│   │   ├── consensus/           # Consensus page
│   │   ├── blocks/              # Blocks explorer
│   │   ├── network/             # Network graph
│   │   ├── ai-engine/           # AI engine dashboard
│   │   ├── metrics/             # Metrics dashboard
│   │   ├── system-health/       # Health monitoring
│   │   ├── investors/           # Investor pitch view
│   │   └── api/                 # API routes (mock data)
│   │
│   ├── components/              # React components
│   │   ├── ui/                  # shadcn/ui base components
│   │   ├── hero-kpi-strip.tsx   # KPI tiles
│   │   ├── node-status-grid.tsx # Node status
│   │   ├── pbft-live-visualizer.tsx # Consensus animation
│   │   ├── animated-network-graph.tsx # P2P visualization
│   │   ├── sidebar-nav.tsx      # Navigation sidebar
│   │   └── ...                  # Many more components
│   │
│   ├── lib/                     # Utilities and contexts
│   │   ├── utils.ts             # cn() helper
│   │   ├── api.ts               # API client
│   │   └── mode-context.tsx     # Investor/operator mode
│   │
│   └── hooks/                   # Custom React hooks
│       └── use-mobile.ts        # Mobile detection
│
├── app/                         # Legacy components (v0 generated)
│   ├── globals.css              # Global styles
│   ├── overview.tsx             # Overview page
│   ├── threats.tsx              # Threats page
│   └── consensus.tsx            # Consensus page
│
├── .env.local                   # Environment variables (local)
├── .env.example                 # Environment template
├── components.json              # shadcn/ui config
├── tailwind.config.ts           # Tailwind CSS config
└── package.json                 # Dependencies
```

---

## 🔌 Connecting to Backend

The dashboard expects the CyberMesh backend to be running on port **8443** (default).

### Backend API Endpoints (Expected)

The frontend makes API calls to:

```
GET  /api/nodes/health          # Node health status
GET  /api/consensus/status      # Consensus status
GET  /api/blocks                # Recent blocks
GET  /api/threats               # Recent threats
GET  /api/metrics               # System metrics
```

### Mock Data

Mock API routes are included in `src/app/api/` for development without a backend:
- `/api/nodes/health/route.ts`
- `/api/consensus/status/route.ts`
- `/api/blocks/route.ts`

---

## 🎨 Styling & Themes

### Tailwind CSS Classes

The dashboard uses custom CSS variables for theming:

```css
/* Dark mode (default) */
--background: 0 0% 3.9%
--foreground: 0 0% 98%
--status-healthy: 142.1 76.2% 36.3%
--status-warning: 38 92% 50%
--status-critical: 0 72.2% 50.6%
```

### Component Styling

Components use `cn()` utility for class merging:
```tsx
import { cn } from "@/lib/utils"

<div className={cn("base-class", conditionalClass && "extra-class")} />
```

---

## 🧪 Development Tips

### Add New Components

```bash
# Add individual shadcn/ui components
npx shadcn@latest add dialog
npx shadcn@latest add select
npx shadcn@latest add calendar
```

### Customize Components

All shadcn/ui components are in `src/components/ui/` - edit them directly!

### Mode Toggle

The dashboard has two modes:
- **Operator Mode** - Technical details, full monitoring
- **Investor Mode** - High-level KPIs, pitch deck view

Toggle with `useMode()` hook:
```tsx
import { useMode } from "@/lib/mode-context"

const { mode, setMode } = useMode()
```

---

## 📊 Key Features Explained

### 1. Hero KPI Strip
Shows 4 key metrics at the top:
- Total Threats Detected
- Average Confidence Score
- Active Nodes
- System Uptime

### 2. Node Status Grid
Real-time status of 5 validators:
- Node ID and health status
- Block height
- Memory/CPU usage
- P2P connections

### 3. PBFT Live Visualizer
Animated consensus visualization showing:
- Proposal phase
- Voting phase
- Commit phase
- Current view number

### 4. Network Graph
Interactive P2P network topology:
- 5 nodes with connections
- Real-time message flow
- Node health colors

### 5. Detection Engines Grid
ML engine statistics:
- Rules Engine accuracy
- Math Engine performance
- ML Engine confidence
- Ensemble voting results

---

## 🐛 Troubleshooting

### Port Already in Use
```bash
# Kill process on port 3000
npx kill-port 3000
# Or use different port
PORT=3001 npm run dev
```

### Build Errors
```bash
# Clear cache and rebuild
rm -rf .next
npm run build
```

### API Connection Issues
Check `.env.local` has correct backend URL:
```env
NEXT_PUBLIC_API_BASE=http://localhost:8443/api
```

---

## 🚢 Deployment

### Vercel (Recommended)
```bash
# Install Vercel CLI
npm i -g vercel

# Deploy
vercel
```

Set environment variables in Vercel dashboard.

### Docker
```bash
# Build
docker build -t cybermesh-frontend .

# Run
docker run -p 3000:3000 cybermesh-frontend
```

---

## 📚 Additional Resources

- [Next.js Documentation](https://nextjs.org/docs)
- [shadcn/ui Documentation](https://ui.shadcn.com)
- [Tailwind CSS Documentation](https://tailwindcss.com/docs)
- [v0.dev](https://v0.dev) - AI component generator

---

## 🤝 Contributing

When modifying the dashboard:
1. Test in both Operator and Investor modes
2. Ensure responsive design (mobile, tablet, desktop)
3. Use TypeScript types for all props
4. Follow existing component patterns
5. Update this README if adding major features

---

## 📝 Notes

- This frontend was generated from a v0.app link with 74 pre-built components
- All components are fully customizable TypeScript/React code
- Mock API routes are included for development
- Production deployment requires connecting to actual CyberMesh backend

**Status:** ✅ Ready for development and customization!
